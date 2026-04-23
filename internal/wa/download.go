package wa

import (
	"encoding/json"
	"os"
	"strings"

	"go.mau.fi/whatsmeow/proto/waE2E"
)

type statusMeta struct {
	MsgID      string `json:"msg_id"`
	SenderJID  string `json:"sender_jid"`
	PushName   string `json:"push_name,omitempty"`
	ReceivedAt string `json:"received_at"`
	MediaPath  string `json:"media_path,omitempty"`
	Mimetype   string `json:"mimetype,omitempty"`
	Caption    string `json:"caption,omitempty"`
	Text       string `json:"text,omitempty"`
}

// extFromMime maps common WhatsApp media mime types to file extensions.
// Falls back to the provided default for unknown types.
func extFromMime(mime, fallback string) string {
	mime = strings.ToLower(mime)
	if i := strings.IndexByte(mime, ';'); i >= 0 {
		mime = strings.TrimSpace(mime[:i])
	}
	switch mime {
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	case "video/mp4":
		return ".mp4"
	case "video/webm":
		return ".webm"
	case "video/quicktime":
		return ".mov"
	case "audio/ogg", "audio/ogg; codecs=opus":
		return ".ogg"
	case "audio/mpeg", "audio/mp3":
		return ".mp3"
	case "audio/mp4", "audio/aac":
		return ".m4a"
	}
	return fallback
}

// textOf returns the best-effort plain-text payload of a message, handling
// the two common text-status shapes (Conversation vs ExtendedTextMessage).
func textOf(msg *waE2E.Message) string {
	if msg == nil {
		return ""
	}
	if s := msg.GetConversation(); s != "" {
		return s
	}
	if ext := msg.GetExtendedTextMessage(); ext != nil {
		return ext.GetText()
	}
	return ""
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o640)
}
