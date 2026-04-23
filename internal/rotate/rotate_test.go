package rotate

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/ppoloczek/story-saver/internal/storage"
)

func TestRunFlatLayout(t *testing.T) {
	tmp := t.TempDir()
	dataDir := filepath.Join(tmp, "data")
	contactDir := filepath.Join(dataDir, "Philip_4915164300143")
	if err := os.MkdirAll(contactDir, 0o755); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 4, 23, 4, 0, 0, 0, time.UTC)
	mk := func(name string) string {
		p := filepath.Join(contactDir, name)
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	// 112 days old -> pruned
	oldJPG := mk("2026-01-01_120000_ABC.jpg")
	oldJSON := mk("2026-01-01_120000_ABC.json")
	// exactly 90 days old -> kept (inclusive)
	borderJPG := mk("2026-01-23_120000_DEF.jpg")
	// 3 days old -> kept
	recentMP4 := mk("2026-04-20_120000_GHI.mp4")
	// non-date file, e.g. a user's README inside the contact folder -> kept
	note := mk("note.txt")

	// A contact folder that will become empty after pruning, so we can check
	// that the empty folder is also removed.
	emptyContact := filepath.Join(dataDir, "stale_123456")
	if err := os.MkdirAll(emptyContact, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(emptyContact, "2026-01-01_000000_X.json"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	idx, err := storage.OpenIndex(filepath.Join(tmp, "idx.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	if _, err := idx.MarkSeen("old", "s@x", now.AddDate(0, 0, -100).Unix(), "/x"); err != nil {
		t.Fatal(err)
	}
	if _, err := idx.MarkSeen("new", "s@x", now.AddDate(0, 0, -5).Unix(), "/x"); err != nil {
		t.Fatal(err)
	}

	err = Run(Options{
		DataDir:       dataDir,
		RetentionDays: 90,
		Index:         idx,
		Now:           now,
	}, zerolog.Nop())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	for _, path := range []string{oldJPG, oldJSON} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("expected pruned: %s (err=%v)", path, err)
		}
	}
	for _, path := range []string{borderJPG, recentMP4, note} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("expected kept: %s (err=%v)", path, err)
		}
	}
	if _, err := os.Stat(emptyContact); !os.IsNotExist(err) {
		t.Errorf("expected empty contact dir removed, got err=%v", err)
	}
	if _, err := os.Stat(contactDir); err != nil {
		t.Errorf("expected non-empty contact dir to remain: %v", err)
	}

	if has, _ := idx.HasSeen("old", "s@x"); has {
		t.Error("old index row should have been pruned")
	}
	if has, _ := idx.HasSeen("new", "s@x"); !has {
		t.Error("recent index row should remain")
	}
}

func TestRunRetentionZeroIsNoop(t *testing.T) {
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, "data", "someone"), 0o755); err != nil {
		t.Fatal(err)
	}
	leave := filepath.Join(tmp, "data", "someone", "2020-01-01_000000_X.jpg")
	if err := os.WriteFile(leave, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	idx, _ := storage.OpenIndex(filepath.Join(tmp, "idx.db"))
	defer idx.Close()
	if err := Run(Options{
		DataDir:       filepath.Join(tmp, "data"),
		RetentionDays: 0,
		Index:         idx,
	}, zerolog.Nop()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(leave); err != nil {
		t.Errorf("retention=0 should not prune: %v", err)
	}
}

func TestHasDatePrefix(t *testing.T) {
	cases := map[string]bool{
		"2026-04-23_140530_ABC.jpg": true,
		"2026-04-23_":               true,  // minimal valid
		"2026-04-23":                false, // missing trailing _
		"note.txt":                  false,
		"26-04-23_x":                false,
		"2026_04-23_x":              false,
		"2026-04-23X140530":         false,
		"":                          false,
	}
	for s, want := range cases {
		if got := hasDatePrefix(s); got != want {
			t.Errorf("hasDatePrefix(%q) = %v, want %v", s, got, want)
		}
	}
}
