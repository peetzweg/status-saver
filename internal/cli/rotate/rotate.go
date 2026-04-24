// Package rotate implements the `status-saver rotate` subcommand —
// a one-shot retention prune, typically fired by a systemd timer.
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
		fmt.Fprintln(fs.Output(), "Delete archived day-folders older than the configured retention")
		fmt.Fprintln(fs.Output(), "window and prune the dedup index. One-shot; meant for systemd timer.")
		fmt.Fprintln(fs.Output())
		fs.PrintDefaults()
	}
	configPath := fs.String("config", "/etc/status-saver/config.yaml", "path to YAML config")
	_ = fs.Parse(args)

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		return 2
	}
	log := logging.New(cfg.LogLevel)

	idx, err := storage.OpenIndex(cfg.IndexDB)
	if err != nil {
		log.Error().Err(err).Msg("open index db")
		return 2
	}
	defer func() { _ = idx.Close() }()

	if err := rotate.Run(rotate.Options{
		DataDir:       cfg.DataDir,
		RetentionDays: cfg.RetentionDays,
		Index:         idx,
	}, log); err != nil {
		log.Error().Err(err).Msg("rotate failed")
		return 2
	}
	return 0
}
