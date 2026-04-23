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
	"sync"
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

// shutdownDrainTimeout caps how long we wait for in-flight status-archival
// handlers to finish after receiving a termination signal. Big enough that
// a typical image/video download can complete; short enough that a stuck
// handler doesn't block systemd's stop timeout.
const shutdownDrainTimeout = 30 * time.Second

func main() {
	os.Exit(run())
}

func run() int {
	configPath := flag.String("config", "/etc/status-saver/config.yaml", "path to YAML config")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		return 2
	}
	if err := cfg.EnsureDirs(); err != nil {
		fmt.Fprintln(os.Stderr, "ensure dirs:", err)
		return 2
	}
	log := logging.New(cfg.LogLevel)

	rootCtx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	idx, err := storage.OpenIndex(cfg.IndexDB)
	if err != nil {
		log.Error().Err(err).Msg("open index db")
		return 2
	}
	defer idx.Close()

	c, err := wa.Open(rootCtx, cfg.SessionDB, log)
	if err != nil {
		log.Error().Err(err).Msg("open whatsmeow client")
		return 2
	}
	defer c.Close()

	if !c.IsPaired() {
		log.Error().Msg("no paired session found — run status-saver-pair first")
		return 2
	}

	statusHandler := wa.NewStatusHandler(c.WA, cfg.DataDir, idx, log)
	historyHandler := wa.NewHistorySyncHandler(c.WA, statusHandler, log)

	// Track in-flight handler invocations so shutdown can wait for them to
	// finish (graceful drain) instead of yanking the rug mid-download.
	var handlerWg sync.WaitGroup
	c.SetMessageHandler(func(evt interface{}) {
		handlerWg.Add(1)
		defer handlerWg.Done()
		statusHandler.Handle(rootCtx, evt)
		historyHandler.Handle(rootCtx, evt)
	})

	if err := c.WA.Connect(); err != nil {
		log.Error().Err(err).Msg("connect to whatsapp")
		return 2
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

	loggedOut := false
	select {
	case <-rootCtx.Done():
		log.Info().Msg("shutdown signal received — disconnecting and draining handlers")
	case <-c.LoggedOut:
		log.Error().Msg("whatsapp session invalidated — exiting")
		loggedOut = true
	}

	// Disconnect first to stop new events arriving, then wait for any handler
	// currently mid-archive to finish. Atomic file writes mean a cut-off
	// handler still leaves the filesystem consistent, but we'd rather not
	// lose a post we've already downloaded.
	c.WA.Disconnect()
	drainHandlers(&handlerWg, log)

	if loggedOut {
		postLogoutAlert(cfg.AlertWebhook, c.WA.Store.ID.String(), log)
		return 1
	}
	return 0
}

// drainHandlers blocks until every tracked handler goroutine finishes, or
// shutdownDrainTimeout elapses — whichever comes first.
func drainHandlers(wg *sync.WaitGroup, log zerolog.Logger) {
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		log.Info().Msg("handlers drained cleanly")
	case <-time.After(shutdownDrainTimeout):
		log.Warn().
			Dur("timeout", shutdownDrainTimeout).
			Msg("handler drain timeout — a download was likely interrupted mid-flight (atomic writes keep disk consistent)")
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
