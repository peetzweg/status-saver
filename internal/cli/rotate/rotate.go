// Package rotate implements the `status-saver rotate` subcommand —
// a one-shot retention prune, typically fired by a systemd timer but
// also runnable manually.
package rotate

import (
	"flag"
	"fmt"
	"os"

	"github.com/ppoloczek/status-saver/internal/config"
	"github.com/ppoloczek/status-saver/internal/logging"
	"github.com/ppoloczek/status-saver/internal/rotate"
	"github.com/ppoloczek/status-saver/internal/storage"
)

// Run is the entry point for `status-saver rotate`. Returns an exit code.
func Run(args []string) int {
	fs := flag.NewFlagSet("rotate", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage: status-saver rotate [flags]")
		fmt.Fprintln(fs.Output())
		fmt.Fprintln(fs.Output(), "Delete archived files older than the retention window and prune")
		fmt.Fprintln(fs.Output(), "the dedup index. One-shot; typically fired by a systemd timer, but")
		fmt.Fprintln(fs.Output(), "safe to run manually. With --retention-days you can override the")
		fmt.Fprintln(fs.Output(), "config value for one run (useful for ad-hoc pruning without")
		fmt.Fprintln(fs.Output(), "editing config).")
		fmt.Fprintln(fs.Output())
		fs.PrintDefaults()
	}
	configPath := fs.String("config", "/etc/status-saver/config.yaml", "path to YAML config")
	// -1 means "unset, use config value". 0 means "disable pruning for this
	// run even if the config has a positive value". A positive value
	// overrides the config.
	retentionOverride := fs.Int("retention-days", -1, "override config retention_days for this run; 0 = keep everything, N = delete files older than N days")
	_ = fs.Parse(args)

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		return 2
	}
	log := logging.New(cfg.LogLevel)

	retentionDays := cfg.RetentionDays
	if *retentionOverride >= 0 {
		if *retentionOverride != cfg.RetentionDays {
			log.Info().
				Int("config", cfg.RetentionDays).
				Int("override", *retentionOverride).
				Msg("retention_days overridden via --retention-days flag")
		}
		retentionDays = *retentionOverride
	}

	idx, err := storage.OpenIndex(cfg.IndexDB)
	if err != nil {
		log.Error().Err(err).Msg("open index db")
		return 2
	}
	defer func() { _ = idx.Close() }()

	if err := rotate.Run(rotate.Options{
		DataDir:       cfg.DataDir,
		RetentionDays: retentionDays,
		Index:         idx,
	}, log); err != nil {
		log.Error().Err(err).Msg("rotate failed")
		return 2
	}
	return 0
}
