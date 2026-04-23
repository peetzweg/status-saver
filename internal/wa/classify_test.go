package wa

import (
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

func TestClassifyImage(t *testing.T) {
	msg := &waE2E.Message{
		ImageMessage: &waE2E.ImageMessage{
			Mimetype: proto.String("image/jpeg"),
			Caption:  proto.String("at the beach"),
		},
	}
	kind, caption, mime := classify(msg)
	if kind != kindImage {
		t.Errorf("kind = %v, want kindImage", kind)
	}
	if caption != "at the beach" {
		t.Errorf("caption = %q", caption)
	}
	if mime != "image/jpeg" {
		t.Errorf("mime = %q", mime)
	}
}

func TestClassifyVideo(t *testing.T) {
	msg := &waE2E.Message{
		VideoMessage: &waE2E.VideoMessage{
			Mimetype: proto.String("video/mp4"),
			Caption:  proto.String("clip"),
		},
	}
	kind, caption, mime := classify(msg)
	if kind != kindVideo {
		t.Errorf("kind = %v, want kindVideo", kind)
	}
	if caption != "clip" || mime != "video/mp4" {
		t.Errorf("caption/mime unexpected: %q %q", caption, mime)
	}
}

func TestClassifyTextConversation(t *testing.T) {
	msg := &waE2E.Message{Conversation: proto.String("plain text")}
	kind, caption, mime := classify(msg)
	if kind != kindText {
		t.Errorf("kind = %v, want kindText", kind)
	}
	if caption != "" || mime != "" {
		t.Errorf("text status should have no caption/mime, got %q %q", caption, mime)
	}
}

func TestClassifyTextExtended(t *testing.T) {
	msg := &waE2E.Message{
		ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text: proto.String("fancy"),
		},
	}
	if kind, _, _ := classify(msg); kind != kindText {
		t.Errorf("extended text: kind = %v, want kindText", kind)
	}
}

func TestClassifyNilAndEmpty(t *testing.T) {
	if kind, _, _ := classify(nil); kind != kindNone {
		t.Errorf("nil message: kind = %v", kind)
	}
	// Empty message — no media, no text.
	if kind, _, _ := classify(&waE2E.Message{}); kind != kindNone {
		t.Errorf("empty message: kind = %v", kind)
	}
	// Sender-key-distribution-style wrapper (no content of our interest).
	msg := &waE2E.Message{
		SenderKeyDistributionMessage: &waE2E.SenderKeyDistributionMessage{
			GroupID: proto.String("status@broadcast"),
		},
	}
	if kind, _, _ := classify(msg); kind != kindNone {
		t.Errorf("sender-key-distribution: kind = %v, want kindNone (dedup poisoning regression)", kind)
	}
}

func TestContentKindString(t *testing.T) {
	cases := map[contentKind]string{
		kindImage: "image",
		kindVideo: "video",
		kindText:  "text",
		kindNone:  "none",
	}
	for k, want := range cases {
		if got := k.String(); got != want {
			t.Errorf("(%d).String() = %q, want %q", k, got, want)
		}
	}
}

func TestExtFromMime(t *testing.T) {
	cases := []struct {
		mime     string
		fallback string
		want     string
	}{
		{"image/jpeg", ".jpg", ".jpg"},
		{"IMAGE/JPEG", ".jpg", ".jpg"},           // case-insensitive
		{"image/jpeg; charset=utf-8", ".jpg", ".jpg"}, // param stripping
		{"image/png", ".jpg", ".png"},
		{"image/webp", ".jpg", ".webp"},
		{"video/mp4", ".mp4", ".mp4"},
		{"video/webm", ".mp4", ".webm"},
		{"video/quicktime", ".mp4", ".mov"},
		{"audio/ogg; codecs=opus", ".bin", ".ogg"},
		{"application/unknown", ".bin", ".bin"}, // fallback path
		{"", ".jpg", ".jpg"},                    // empty -> fallback
	}
	for _, c := range cases {
		got := extFromMime(c.mime, c.fallback)
		if got != c.want {
			t.Errorf("extFromMime(%q, %q) = %q, want %q", c.mime, c.fallback, got, c.want)
		}
	}
}

func TestTextOfPrefersConversation(t *testing.T) {
	// When both Conversation and ExtendedTextMessage exist, Conversation wins.
	msg := &waE2E.Message{
		Conversation: proto.String("conv"),
		ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text: proto.String("ext"),
		},
	}
	if got := textOf(msg); got != "conv" {
		t.Errorf("textOf = %q, want conv", got)
	}
}

func TestTextOfFallbackToExtended(t *testing.T) {
	msg := &waE2E.Message{
		ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text: proto.String("only extended"),
		},
	}
	if got := textOf(msg); got != "only extended" {
		t.Errorf("textOf = %q", got)
	}
}

func TestTextOfNilAndEmpty(t *testing.T) {
	if got := textOf(nil); got != "" {
		t.Errorf("textOf(nil) = %q", got)
	}
	if got := textOf(&waE2E.Message{}); got != "" {
		t.Errorf("textOf(empty) = %q", got)
	}
}

func TestSenderLabelFor(t *testing.T) {
	jid := types.NewJID("49123456789", types.DefaultUserServer)
	if got := senderLabelFor(jid, "Alice"); got != "Alice_49123456789" {
		t.Errorf("with push name: %q", got)
	}
	if got := senderLabelFor(jid, ""); got != "49123456789" {
		t.Errorf("without push name: %q", got)
	}
}
