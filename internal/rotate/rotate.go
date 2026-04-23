package rotate

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/rs/zerolog"

	"github.com/ppoloczek/story-saver/internal/storage"
)

// Options holds the knobs needed for a single rotation pass.
type Options struct {
	DataDir       string
	RetentionDays int
	Index         *storage.Index
	Now           time.Time // injected for testability
}

// Run removes day-folders older than RetentionDays and prunes the dedup index
// accordingly. RetentionDays == 0 is a no-op (archive forever).
func Run(opt Options, log zerolog.Logger) error {
	if opt.RetentionDays == 0 {
		log.Info().Msg("retention disabled — nothing to prune")
		return nil
	}
	if opt.Now.IsZero() {
		opt.Now = time.Now()
	}
	cutoff := opt.Now.AddDate(0, 0, -opt.RetentionDays)

	entries, err := os.ReadDir(opt.DataDir)
	if err != nil {
		if os.IsNotExist(err) {
			log.Info().Str("dir", opt.DataDir).Msg("data dir missing — nothing to prune")
			return nil
		}
		return fmt.Errorf("read data dir: %w", err)
	}

	var removedDirs, keptDirs int
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		day, err := time.Parse("2006-01-02", e.Name())
		if err != nil {
			// Non-date folder (e.g. archive/, tmp/) — leave alone.
			continue
		}
		if !day.Before(cutoff.Truncate(24 * time.Hour)) {
			keptDirs++
			continue
		}
		path := filepath.Join(opt.DataDir, e.Name())
		if err := os.RemoveAll(path); err != nil {
			log.Error().Err(err).Str("path", path).Msg("remove day dir")
			continue
		}
		removedDirs++
		log.Info().Str("path", path).Msg("pruned day dir")
	}

	n, err := opt.Index.PruneOlderThan(cutoff.Unix())
	if err != nil {
		return fmt.Errorf("prune index: %w", err)
	}
	log.Info().
		Int("removed_dirs", removedDirs).
		Int("kept_dirs", keptDirs).
		Int64("removed_index_rows", n).
		Time("cutoff", cutoff).
		Msg("rotation complete")
	return nil
}
