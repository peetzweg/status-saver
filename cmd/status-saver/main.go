// Command status-saver is the long-running daemon that listens for incoming
// WhatsApp status@broadcast messages and archives them to disk. Expects a
// session already paired via status-saver-pair.
package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"

	"github.com/ppoloczek/status-saver/internal/config"
	"github.com/ppoloczek/status-saver/internal/logging"
	"github.com/ppoloczek/status-saver/internal/storage"
	"github.com/ppoloczek/status-saver/internal/wa"
)

// recentStatusRequestCount is how many status posts we ask the phone to
// replay on daemon startup. 50 is the value recommended in whatsmeow's
// BuildHistorySyncRequest doc comment.
const recentStatusRequestCount = 50

func main() {
	configPath := flag.String("config", "/etc/status-saver/config.yaml", "path to YAML config")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		os.Exit(2)
	}
	if err := cfg.EnsureDirs(); err != nil {
		fmt.Fprintln(os.Stderr, "ensure dirs:", err)
		os.Exit(2)
	}
	log := logging.New(cfg.LogLevel)

	rootCtx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	idx, err := storage.OpenIndex(cfg.IndexDB)
	if err != nil {
		log.Fatal().Err(err).Msg("open index db")
	}
	defer idx.Close()

	c, err := wa.Open(rootCtx, cfg.SessionDB, log)
	if err != nil {
		log.Fatal().Err(err).Msg("open whatsmeow client")
	}
	defer c.Close()

	if !c.IsPaired() {
		log.Fatal().Msg("no paired session found — run status-saver-pair first")
	}

	statusHandler := wa.NewStatusHandler(c.WA, cfg.DataDir, idx, log)
	historyHandler := wa.NewHistorySyncHandler(c.WA, statusHandler, log)
	c.SetMessageHandler(func(evt interface{}) {
		statusHandler.Handle(rootCtx, evt)
		historyHandler.Handle(rootCtx, evt)
	})

	if err := c.WA.Connect(); err != nil {
		log.Fatal().Err(err).Msg("connect to whatsapp")
	}
	log.Info().Str("jid", c.WA.Store.ID.String()).Msg("daemon started — awaiting status broadcasts")

	// Fire a best-effort status-backfill request at the phone 5s after connect.
	// Reliability is poor: it only works when the phone is online AND decides
	// to respond, which defeats headless server deployment. Kept because when
	// it does fire, it's free. See whatsmeow/discussions/1033 for the wider
	// context on why no reliable server-driven backfill exists yet.
	go func() {
		select {
		case <-time.After(5 * time.Second):
		case <-rootCtx.Done():
			return
		}
		if err := c.RequestRecentStatuses(rootCtx, recentStatusRequestCount); err != nil {
			log.Warn().Err(err).Msg("best-effort status backfill request failed — continuing with live capture only")
		}
	}()

	select {
	case <-rootCtx.Done():
		log.Info().Msg("signal received — shutting down")
	case <-c.LoggedOut:
		log.Error().Msg("whatsapp session invalidated — exiting")
		postLogoutAlert(cfg.AlertWebhook, c.WA.Store.ID.String(), log)
		// Close deferred, then exit non-zero so systemd surfaces the failure.
		os.Exit(1)
	}
}

// postLogoutAlert fires a best-effort POST to the configured webhook. Silent
// if AlertWebhook is empty. Short timeout so we never hang shutdown.
func postLogoutAlert(webhook, jid string, log zerolog.Logger) {
	if webhook == "" {
		return
	}
	body := fmt.Sprintf("status-saver: WhatsApp session for %s was logged out at %s",
		jid, time.Now().Format(time.RFC3339))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhook, bytes.NewBufferString(body))
	if err != nil {
		log.Warn().Err(err).Msg("build alert request")
		return
	}
	req.Header.Set("Content-Type", "text/plain")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Warn().Err(err).Msg("post alert webhook")
		return
	}
	resp.Body.Close()
}
