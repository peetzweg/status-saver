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

	"github.com/ppoloczek/status-saver/internal/storage"
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

	// Classify upfront. whatsmeow often dispatches two events for the same
	// msgID on a status post: first an infrastructure wrapper (sender-key
	// distribution, ephemeral setting, etc.) with no user-visible content,
	// then the actual payload. Marking the first dispatch as seen would
	// cause the second one — the real image/video/text — to be rejected
	// as a duplicate. So: classify first, and skip empty dispatches
	// without touching the index.
	kind, caption, mime := classify(evt.Message)
	if kind == kindNone {
		log.Debug().Msg("status event has no user-visible content — skipping (likely sender-key distribution)")
		return
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
		Caption:    caption,
		Mimetype:   mime,
	}
	storedPath := ""

	switch kind {
	case kindImage, kindVideo:
		mediaPath, err := h.downloadMedia(ctx, evt.Message, base)
		if err != nil {
			log.Error().Err(err).Msg("media download failed")
			return
		}
		meta.MediaPath = mediaPath
		storedPath = mediaPath
	case kindText:
		text := textOf(evt.Message)
		meta.Text = text
		txtPath := base + ".txt"
		if err := os.WriteFile(txtPath, []byte(text), 0o640); err != nil {
			log.Error().Err(err).Msg("write text failed")
			return
		}
		storedPath = txtPath
	}

	if err := writeJSON(jsonPath, meta); err != nil {
		log.Error().Err(err).Msg("write metadata failed")
		return
	}

	inserted, err := h.index.MarkSeen(msgID, senderJID, ts.Unix(), storedPath)
	if err != nil {
		log.Error().Err(err).Msg("mark seen failed")
		return
	}
	if inserted {
		log.Info().
			Str("kind", kind.String()).
			Str("path", storedPath).
			Msg("status archived")
	}
}

type contentKind int

const (
	kindNone contentKind = iota
	kindImage
	kindVideo
	kindText
)

func (k contentKind) String() string {
	switch k {
	case kindImage:
		return "image"
	case kindVideo:
		return "video"
	case kindText:
		return "text"
	default:
		return "none"
	}
}

// classify inspects a decoded whatsmeow message and reports the kind of
// user-visible content it carries, plus any caption/mimetype. kindNone
// means the message is an infrastructure wrapper (e.g. sender-key
// distribution) and should be skipped entirely.
func classify(msg *waE2E.Message) (kind contentKind, caption, mime string) {
	if msg == nil {
		return kindNone, "", ""
	}
	if img := msg.GetImageMessage(); img != nil {
		return kindImage, img.GetCaption(), img.GetMimetype()
	}
	if vid := msg.GetVideoMessage(); vid != nil {
		return kindVideo, vid.GetCaption(), vid.GetMimetype()
	}
	if txt := textOf(msg); txt != "" {
		return kindText, "", ""
	}
	return kindNone, "", ""
}

// downloadMedia saves the media from msg next to base. Caller guarantees msg
// carries an ImageMessage or VideoMessage (classify() returned kindImage or
// kindVideo). Returns the absolute path of the written file.
func (h *StatusHandler) downloadMedia(ctx context.Context, msg *waE2E.Message, base string) (string, error) {
	if img := msg.GetImageMessage(); img != nil {
		return h.save(ctx, img, base+extFromMime(img.GetMimetype(), ".jpg"))
	}
	if vid := msg.GetVideoMessage(); vid != nil {
		return h.save(ctx, vid, base+extFromMime(vid.GetMimetype(), ".mp4"))
	}
	return "", fmt.Errorf("downloadMedia called on non-media message")
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
