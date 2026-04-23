package storage

import (
	"strings"
	"testing"
	"time"
)

func TestPathForFlatLayout(t *testing.T) {
	ts := time.Date(2026, 4, 23, 14, 5, 30, 0, time.UTC)
	base, js := PathFor("/var/lib/ss/data", ts, "+49 170 / 1234", "msg:ABC/XY")

	// Contact folder is immediately under data dir (no per-day directory).
	if !strings.Contains(base, "/data/_49_170_1234/") {
		t.Errorf("base missing sanitized contact folder directly under data dir: %s", base)
	}
	// Filename stem contains date + time + msgid, sortable.
	if !strings.HasSuffix(base, "/2026-04-23_140530_msg_ABC_XY") {
		t.Errorf("base stem mismatch: %s", base)
	}
	if js != base+".json" {
		t.Errorf("json path mismatch: %s vs %s", js, base+".json")
	}
}

func TestSanitizeEmpty(t *testing.T) {
	if got := sanitize(""); got != "unknown" {
		t.Errorf("empty sanitize = %q, want unknown", got)
	}
	if got := sanitize("  "); got != "unknown" {
		t.Errorf("whitespace sanitize = %q, want unknown", got)
	}
}
