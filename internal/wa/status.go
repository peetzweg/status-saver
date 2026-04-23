package wa

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/rs/zerolog"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"github.com/ppoloczek/story-saver/internal/storage"
)

// StatusHandler processes status@broadcast message events: dedup, media
// download, metadata sidecar write, index update.
type StatusHandler struct {
	client  *whatsmeow.Client
	dataDir string
	index   *storage.Index
	log     zerolog.Logger
}

func NewStatusHandler(c *whatsmeow.Client, dataDir string, idx *storage.Index, log zerolog.Logger) *StatusHandler {
	return &StatusHandler{
		client:  c,
		dataDir: dataDir,
		index:   idx,
		log:     log.With().Str("mod", "status").Logger(),
	}
}

// Handle inspects an arbitrary whatsmeow event. If it is a status-broadcast
// message it is archived. Other events are ignored so this is safe to wire
// directly into AddEventHandler-style dispatchers.
func (h *StatusHandler) Handle(ctx context.Context, evt interface{}) {
	msgEvt, ok := evt.(*events.Message)
	if !ok {
		return
	}
	if msgEvt.Info.Chat != types.StatusBroadcastJID {
		return
	}
	h.archive(ctx, msgEvt)
}

func (h *StatusHandler) archive(ctx context.Context, evt *events.Message) {
	senderJID := evt.Info.Sender.String()
	msgID := string(evt.Info.ID)
	ts := evt.Info.Timestamp
	log := h.log.With().Str("sender", senderJID).Str("msgid", msgID).Logger()

	if ts.IsZero() {
		ts = time.Now()
	}

	seen, err := h.index.HasSeen(msgID, senderJID)
	if err != nil {
		log.Error().Err(err).Msg("index lookup failed")
		return
	}
	if seen {
		log.Debug().Msg("duplicate status, skipping")
		return
	}

	senderLabel := senderLabelFor(evt.Info.Sender, evt.Info.PushName)
	base, jsonPath := storage.PathFor(h.dataDir, ts, senderLabel, msgID)
	if err := os.MkdirAll(filepath.Dir(base), 0o750); err != nil {
		log.Error().Err(err).Msg("mkdir failed")
		return
	}

	meta := statusMeta{
		MsgID:      msgID,
		SenderJID:  senderJID,
		PushName:   evt.Info.PushName,
		ReceivedAt: ts.Format(time.RFC3339),
	}
	storedPath := ""

	mediaPath, mime, caption, err := h.downloadMedia(ctx, evt.Message, base)
	if err != nil {
		log.Error().Err(err).Msg("media download failed")
		return
	}
	switch {
	case mediaPath != "":
		meta.MediaPath = mediaPath
		meta.Mimetype = mime
		meta.Caption = caption
		storedPath = mediaPath
	default:
		text := textOf(evt.Message)
		meta.Text = text
		if text == "" {
			log.Debug().Msg("status has neither media nor text — metadata only")
		} else {
			txtPath := base + ".txt"
			if err := os.WriteFile(txtPath, []byte(text), 0o640); err != nil {
				log.Error().Err(err).Msg("write text failed")
				return
			}
			storedPath = txtPath
		}
	}

	if err := writeJSON(jsonPath, meta); err != nil {
		log.Error().Err(err).Msg("write metadata failed")
		return
	}
	if storedPath == "" {
		storedPath = jsonPath
	}

	inserted, err := h.index.MarkSeen(msgID, senderJID, ts.Unix(), storedPath)
	if err != nil {
		log.Error().Err(err).Msg("mark seen failed")
		return
	}
	if inserted {
		log.Info().Str("path", storedPath).Msg("status archived")
	}
}

// downloadMedia pulls media out of the message, if any, and writes it next to
// base. Returns (path, mime, caption). All empty if the status is text-only.
func (h *StatusHandler) downloadMedia(ctx context.Context, msg *waE2E.Message, base string) (string, string, string, error) {
	if msg == nil {
		return "", "", "", nil
	}
	if img := msg.GetImageMessage(); img != nil {
		mime := img.GetMimetype()
		path, err := h.save(ctx, img, base+extFromMime(mime, ".jpg"))
		return path, mime, img.GetCaption(), err
	}
	if vid := msg.GetVideoMessage(); vid != nil {
		mime := vid.GetMimetype()
		path, err := h.save(ctx, vid, base+extFromMime(mime, ".mp4"))
		return path, mime, vid.GetCaption(), err
	}
	return "", "", "", nil
}

func (h *StatusHandler) save(ctx context.Context, m whatsmeow.DownloadableMessage, path string) (string, error) {
	data, err := h.client.Download(ctx, m)
	if err != nil {
		return "", fmt.Errorf("download: %w", err)
	}
	if err := os.WriteFile(path, data, 0o640); err != nil {
		return "", fmt.Errorf("write %s: %w", path, err)
	}
	return path, nil
}

// senderLabelFor produces a stable filesystem-safe folder name for a sender,
// combining push name (if present) with phone number part of the JID.
// Sanitization happens inside storage.PathFor.
func senderLabelFor(jid types.JID, pushName string) string {
	if pushName == "" {
		return jid.User
	}
	return pushName + "_" + jid.User
}
