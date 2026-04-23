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
// Requires the phone to be online and willing to respond. There's no
// synchronous success signal — watch the logs for "history sync received
// type=ON_DEMAND" shortly after.
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
		Msg("requested recent status history from phone — response will arrive as HistorySync event")
	return nil
}
