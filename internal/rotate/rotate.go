package rotate

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/rs/zerolog"

	"github.com/ppoloczek/status-saver/internal/storage"
)

// Options holds the knobs needed for a single rotation pass.
type Options struct {
	DataDir       string
	RetentionDays int
	Index         *storage.Index
	Now           time.Time // injected for testability
}

// Run deletes archived files older than RetentionDays and prunes the dedup
// index. RetentionDays == 0 is a no-op (archive forever).
//
// Flat-layout walker: data/<contact>/<YYYY-MM-DD_HHMMSS>_<msgid>.<ext>. The
// YYYY-MM-DD prefix on each filename drives pruning; non-matching names are
// left alone so manually dropped notes/archives coexist safely.
func Run(opt Options, log zerolog.Logger) error {
	if opt.RetentionDays == 0 {
		log.Info().Msg("retention disabled — nothing to prune")
		return nil
	}
	now := opt.Now
	if now.IsZero() {
		now = time.Now()
	}
	cutoff := now.AddDate(0, 0, -opt.RetentionDays)
	cutoffDay := cutoff.Format("2006-01-02")

	contactEntries, err := os.ReadDir(opt.DataDir)
	if err != nil {
		if os.IsNotExist(err) {
			log.Info().Str("dir", opt.DataDir).Msg("data dir missing — nothing to prune")
			return nil
		}
		return fmt.Errorf("read data dir: %w", err)
	}

	var removedFiles, keptFiles, removedContacts int
	for _, ce := range contactEntries {
		if !ce.IsDir() {
			continue
		}
		contactPath := filepath.Join(opt.DataDir, ce.Name())
		files, err := os.ReadDir(contactPath)
		if err != nil {
			log.Error().Err(err).Str("path", contactPath).Msg("read contact dir")
			continue
		}
		keptInContact := 0
		for _, f := range files {
			if f.IsDir() {
				keptInContact++
				continue
			}
			name := f.Name()
			if !hasDatePrefix(name) {
				keptFiles++
				keptInContact++
				continue
			}
			if name[:10] >= cutoffDay {
				keptFiles++
				keptInContact++
				continue
			}
			full := filepath.Join(contactPath, name)
			if err := os.Remove(full); err != nil {
				log.Error().Err(err).Str("path", full).Msg("remove file")
				keptInContact++
				continue
			}
			removedFiles++
			log.Debug().Str("path", full).Msg("pruned file")
		}
		if keptInContact == 0 {
			if err := os.Remove(contactPath); err == nil {
				removedContacts++
			}
		}
	}

	n, err := opt.Index.PruneOlderThan(cutoff.Unix())
	if err != nil {
		return fmt.Errorf("prune index: %w", err)
	}
	log.Info().
		Int("removed_files", removedFiles).
		Int("kept_files", keptFiles).
		Int("removed_contacts", removedContacts).
		Int64("removed_index_rows", n).
		Str("cutoff", cutoffDay).
		Msg("rotation complete")
	return nil
}

// hasDatePrefix reports whether s starts with "YYYY-MM-DD_". Anything not
// matching this shape is left untouched by the rotation walker.
func hasDatePrefix(s string) bool {
	if len(s) < 11 || s[4] != '-' || s[7] != '-' || s[10] != '_' {
		return false
	}
	for _, i := range [...]int{0, 1, 2, 3, 5, 6, 8, 9} {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
