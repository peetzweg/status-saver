// Command status-saver-rotate removes archived day-folders older than the
// configured retention window and prunes the dedup index. Meant to be fired
// from a systemd timer around 04:00 local time.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/ppoloczek/status-saver/internal/config"
	"github.com/ppoloczek/status-saver/internal/logging"
	"github.com/ppoloczek/status-saver/internal/rotate"
	"github.com/ppoloczek/status-saver/internal/storage"
)

func main() {
	configPath := flag.String("config", "/etc/status-saver/config.yaml", "path to YAML config")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		os.Exit(2)
	}
	log := logging.New(cfg.LogLevel)

	idx, err := storage.OpenIndex(cfg.IndexDB)
	if err != nil {
		log.Fatal().Err(err).Msg("open index db")
	}
	defer func() { _ = idx.Close() }()

	if err := rotate.Run(rotate.Options{
		DataDir:       cfg.DataDir,
		RetentionDays: cfg.RetentionDays,
		Index:         idx,
	}, log); err != nil {
		log.Fatal().Err(err).Msg("rotate failed")
	}
}
