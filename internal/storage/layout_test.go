package storage

import (
	"strings"
	"testing"
	"time"
)

func TestPathForSanitizes(t *testing.T) {
	ts := time.Date(2026, 4, 23, 14, 5, 30, 0, time.UTC)
	base, js := PathFor("/var/lib/ss/data", ts, "+49 170 / 1234", "msg:ABC/XY")

	if !strings.Contains(base, "2026-04-23") {
		t.Errorf("base missing date: %s", base)
	}
	if !strings.Contains(base, "_49_170_1234") {
		t.Errorf("base missing sanitized sender: %s", base)
	}
	if !strings.Contains(base, "140530_msg_ABC_XY") {
		t.Errorf("base missing hms + sanitized msgid: %s", base)
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
