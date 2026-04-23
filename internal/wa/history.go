package wa

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

// HistorySyncHandler funnels status@broadcast messages found inside
// *events.HistorySync blobs through the normal StatusHandler. whatsmeow
// receives these blobs when the phone proactively pushes historical
// conversation data (typically on first connect after pairing and on
// reconnects). It lets us catch statuses posted while the daemon was
// offline — but only subject to what the phone decides to include, and
// only while the phone is itself online.
type HistorySyncHandler struct {
	client *whatsmeow.Client
	status *StatusHandler
	log    zerolog.Logger
}

func NewHistorySyncHandler(c *whatsmeow.Client, s *StatusHandler, log zerolog.Logger) *HistorySyncHandler {
	return &HistorySyncHandler{
		client: c,
		status: s,
		log:    log.With().Str("mod", "history").Logger(),
	}
}

// Handle is safe to register alongside StatusHandler.Handle on the same
// event dispatcher — it no-ops on any event other than *events.HistorySync.
func (h *HistorySyncHandler) Handle(ctx context.Context, evt interface{}) {
	hs, ok := evt.(*events.HistorySync)
	if !ok || hs.Data == nil {
		return
	}
	data := hs.Data

	convs := data.GetConversations()
	h.log.Info().
		Str("type", data.GetSyncType().String()).
		Uint32("chunk", data.GetChunkOrder()).
		Uint32("progress", data.GetProgress()).
		Int("conversations", len(convs)).
		Msg("history sync received")

	var statusCount, replayed int
	for _, conv := range convs {
		chatJID, err := types.ParseJID(conv.GetID())
		if err != nil {
			continue
		}
		if chatJID != types.StatusBroadcastJID {
			continue
		}
		msgs := conv.GetMessages()
		statusCount += len(msgs)
		for _, m := range msgs {
			webMsg := m.GetMessage()
			if webMsg == nil {
				continue
			}
			parsed, err := h.client.ParseWebMessage(chatJID, webMsg)
			if err != nil {
				h.log.Warn().Err(err).Msg("parse web message from history sync")
				continue
			}
			// Route through the normal status handler: it dedupes, skips
			// empty wrappers, and does the same archive logic we use for
			// live events.
			h.status.Handle(ctx, parsed)
			replayed++
		}
	}
	if statusCount > 0 {
		h.log.Info().
			Int("status_messages_in_blob", statusCount).
			Int("routed_to_handler", replayed).
			Msg("processed status broadcasts from history sync")
	}
}

// RequestRecentStatuses asks the primary device (the phone) to push back the
// last `count` status posts via an ON_DEMAND HistorySync. Response arrives
// asynchronously as an *events.HistorySync with SyncType=ON_DEMAND; our
// HistorySyncHandler picks it up through the normal event bus.
//
// IMPORTANT: this is best-effort and unreliable. It requires the phone to be
// online AND cooperative, which defeats the point of running headless on a
// server. In practice the phone often ACKs the peer message but sends no
// HistorySync response. Kept because it occasionally works and has no cost
// when it doesn't, but it is NOT a reliable gap-fill for missed statuses.
//
// Context: WhatsApp Desktop shows prior-posted statuses after being fully
// quit, implying a server-driven mechanism that none of the open-source
// libraries (whatsmeow, Baileys, whatsapp-web.js) have reverse-engineered.
// See https://github.com/tulir/whatsmeow/discussions/1033 — filed, unanswered.
// The only reliable strategy for status-saver is 24/7 daemon uptime.
func (c *Client) RequestRecentStatuses(ctx context.Context, count int) error {
	// Anchor: status@broadcast chat, "now" as the upper boundary. The phone
	// interprets this as "give me the last `count` status messages older
	// than now". OldestMsgID is intentionally empty — we haven't anchored
	// on any particular message.
	anchor := &types.MessageInfo{
		MessageSource: types.MessageSource{
			Chat:     types.StatusBroadcastJID,
			IsFromMe: false,
		},
		Timestamp: time.Now(),
	}
	msg := c.WA.BuildHistorySyncRequest(anchor, count)
	resp, err := c.WA.SendPeerMessage(ctx, msg)
	if err != nil {
		return fmt.Errorf("send history-sync request: %w", err)
	}
	c.Log.Info().
		Str("req_id", resp.ID).
		Int("count", count).
		Msg("best-effort status backfill request sent to phone — may not produce any response")
	return nil
}
