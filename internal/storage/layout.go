package storage

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var unsafeChars = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func sanitize(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "unknown"
	}
	return unsafeChars.ReplaceAllString(s, "_")
}

// PathFor returns the base path (without extension) and the JSON sidecar path
// for a given status post. Caller appends the media extension (.jpg/.mp4/.txt).
//
// Flat layout, one folder per contact:
//
//	<dataDir>/<sender>/<YYYY-MM-DD_HHMMSS>_<msgid>
//
// The date/time prefix keeps files sortable within a contact folder while
// avoiding the extra nesting of a per-day directory tree. Sender and msgID
// are sanitized to safe filename characters.
func PathFor(dataDir string, t time.Time, sender, msgID string) (base, jsonPath string) {
	dir := filepath.Join(dataDir, sanitize(sender))
	stem := fmt.Sprintf("%s_%s", t.Format("2006-01-02_150405"), sanitize(msgID))
	base = filepath.Join(dir, stem)
	jsonPath = base + ".json"
	return
}
