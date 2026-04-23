// Command status-saver-pair performs a one-shot QR pairing against WhatsApp
// and writes the resulting session into the configured SQLite store. Run this
// interactively on the server (or via ssh) before starting the daemon.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mdp/qrterminal/v3"
	"go.mau.fi/whatsmeow"

	"github.com/ppoloczek/status-saver/internal/config"
	"github.com/ppoloczek/status-saver/internal/logging"
	"github.com/ppoloczek/status-saver/internal/wa"
)

// postPairGrace is how long we keep the WebSocket open after the pair-success
// IQ arrives. WhatsApp needs this window to push app-state keys, contacts, and
// initial sync messages before the phone app marks the device as "linked".
// Disconnecting sooner leaves the phone stuck on "pairing…".
const postPairGrace = 30 * time.Second

func main() {
	configPath := flag.String("config", "/etc/status-saver/config.yaml", "path to YAML config")
	force := flag.Bool("force", false, "delete existing session.db and re-pair from scratch")
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

	if *force {
		if err := os.Remove(cfg.SessionDB); err != nil && !os.IsNotExist(err) {
			log.Fatal().Err(err).Str("path", cfg.SessionDB).Msg("force: remove session.db")
		}
		log.Info().Str("path", cfg.SessionDB).Msg("force: removed existing session.db — starting fresh")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	c, err := wa.Open(ctx, cfg.SessionDB, log)
	if err != nil {
		log.Fatal().Err(err).Msg("open whatsmeow client")
	}
	defer c.Close()

	if c.IsPaired() {
		log.Info().
			Str("jid", c.WA.Store.ID.String()).
			Msg("already paired — pass --force to delete the session and re-pair")
		return
	}

	qrChan, err := c.WA.GetQRChannel(ctx)
	if err != nil {
		log.Fatal().Err(err).Msg("get qr channel")
	}
	if err := c.WA.Connect(); err != nil {
		log.Fatal().Err(err).Msg("connect")
	}

	fmt.Println()
	fmt.Println("Open WhatsApp on the phone that owns the target account:")
	fmt.Println("  Settings -> Linked Devices -> Link a Device")
	fmt.Println("Scan the QR code below. A fresh code is redrawn until pairing completes.")
	fmt.Println()

	if err := waitForPairing(ctx, qrChan); err != nil {
		log.Fatal().Err(err).Msg("pairing failed")
	}
	log.Info().
		Str("jid", c.WA.Store.ID.String()).
		Dur("grace", postPairGrace).
		Msg("pair-success received; keeping connection open for post-pair handshake")

	graceCtx, graceCancel := context.WithTimeout(ctx, postPairGrace)
	defer graceCancel()
	<-graceCtx.Done()
	log.Info().Msg("pairing complete — session stored, disconnecting")
}

func waitForPairing(ctx context.Context, qrChan <-chan whatsmeow.QRChannelItem) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case evt, ok := <-qrChan:
			if !ok {
				return errors.New("qr channel closed without success")
			}
			switch evt.Event {
			case whatsmeow.QRChannelEventCode:
				qrterminal.GenerateHalfBlock(evt.Code, qrterminal.L, os.Stdout)
				fmt.Printf("  (code refreshes automatically; %s)\n\n", time.Now().Format("15:04:05"))
			case "success":
				return nil
			case "timeout":
				return errors.New("QR timed out before being scanned")
			case "err-client-outdated":
				return errors.New("whatsmeow reports client-outdated — update the library")
			case "err-scanned-without-multidevice":
				return errors.New("phone does not have multi-device enabled")
			default:
				return fmt.Errorf("unexpected qr event: %s", evt.Event)
			}
		}
	}
}
