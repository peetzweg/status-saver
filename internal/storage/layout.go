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
//	Layout: <dataDir>/YYYY-MM-DD/<sender>/<HHMMSS>_<msgid>
//
// Both sender and msgID are sanitized to safe filename characters.
func PathFor(dataDir string, t time.Time, sender, msgID string) (base, jsonPath string) {
	day := t.Format("2006-01-02")
	hms := t.Format("150405")
	dir := filepath.Join(dataDir, day, sanitize(sender))
	base = filepath.Join(dir, fmt.Sprintf("%s_%s", hms, sanitize(msgID)))
	jsonPath = base + ".json"
	return
}
