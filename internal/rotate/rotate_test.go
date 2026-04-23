package rotate

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/ppoloczek/story-saver/internal/storage"
)

func TestRunRemovesOldDirs(t *testing.T) {
	tmp := t.TempDir()
	dataDir := filepath.Join(tmp, "data")

	// Create day-folders relative to a fixed "now".
	now := time.Date(2026, 4, 23, 4, 0, 0, 0, time.UTC)
	mk := func(day string) string {
		d := filepath.Join(dataDir, day)
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "dummy.json"), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
		return d
	}
	oldDir := mk("2026-01-01")    // 112 days old → pruned
	borderDir := mk("2026-01-23") // exactly 90 days → kept (retention is inclusive)
	recentDir := mk("2026-04-20") // 3 days old → kept
	nonDateDir := filepath.Join(dataDir, "archive")
	if err := os.MkdirAll(nonDateDir, 0o755); err != nil {
		t.Fatal(err)
	}

	idx, err := storage.OpenIndex(filepath.Join(tmp, "idx.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	// Seed one row older than cutoff to verify PruneOlderThan is called.
	oldTs := now.AddDate(0, 0, -100).Unix()
	if _, err := idx.MarkSeen("m1", "s@x", oldTs, "/x"); err != nil {
		t.Fatal(err)
	}
	newTs := now.AddDate(0, 0, -5).Unix()
	if _, err := idx.MarkSeen("m2", "s@x", newTs, "/x"); err != nil {
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

	if _, err := os.Stat(oldDir); !os.IsNotExist(err) {
		t.Errorf("old dir should be gone: %v", err)
	}
	if _, err := os.Stat(borderDir); err != nil {
		t.Errorf("border dir (exactly at cutoff) should be kept: %v", err)
	}
	if _, err := os.Stat(recentDir); err != nil {
		t.Errorf("recent dir should remain: %v", err)
	}
	if _, err := os.Stat(nonDateDir); err != nil {
		t.Errorf("non-date dir should remain: %v", err)
	}

	has, _ := idx.HasSeen("m1", "s@x")
	if has {
		t.Error("old index row should have been pruned")
	}
	has, _ = idx.HasSeen("m2", "s@x")
	if !has {
		t.Error("recent index row should remain")
	}
}

func TestRunRetentionZeroIsNoop(t *testing.T) {
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, "data", "2020-01-01"), 0o755); err != nil {
		t.Fatal(err)
	}
	idx, _ := storage.OpenIndex(filepath.Join(tmp, "idx.db"))
	defer idx.Close()
	err := Run(Options{
		DataDir:       filepath.Join(tmp, "data"),
		RetentionDays: 0,
		Index:         idx,
	}, zerolog.Nop())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(tmp, "data", "2020-01-01")); err != nil {
		t.Errorf("retention=0 should not prune: %v", err)
	}
}
