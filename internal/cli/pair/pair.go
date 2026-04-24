// Package pair implements the `status-saver pair` subcommand — a one-shot
// interactive QR pairing that writes the resulting session into the
// configured SQLite store.
package pair

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

// Run is the entry point for `status-saver pair`. Returns an exit code.
func Run(args []string) int {
	fs := flag.NewFlagSet("pair", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage: status-saver pair [flags]")
		fmt.Fprintln(fs.Output())
		fmt.Fprintln(fs.Output(), "Interactive QR pairing against a WhatsApp account. Prints QR")
		fmt.Fprintln(fs.Output(), "codes to the terminal until the phone scans one.")
		fmt.Fprintln(fs.Output())
		fs.PrintDefaults()
	}
	configPath := fs.String("config", "/etc/status-saver/config.yaml", "path to YAML config")
	force := fs.Bool("force", false, "delete existing session.db and re-pair from scratch")
	_ = fs.Parse(args)

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

	if *force {
		if err := os.Remove(cfg.SessionDB); err != nil && !os.IsNotExist(err) {
			log.Error().Err(err).Str("path", cfg.SessionDB).Msg("force: remove session.db")
			return 2
		}
		log.Info().Str("path", cfg.SessionDB).Msg("force: removed existing session.db — starting fresh")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	c, err := wa.Open(ctx, cfg.SessionDB, log)
	if err != nil {
		log.Error().Err(err).Msg("open whatsmeow client")
		return 2
	}
	defer c.Close()

	if c.IsPaired() {
		log.Info().
			Str("jid", c.WA.Store.ID.String()).
			Msg("already paired — pass --force to delete the session and re-pair")
		return 0
	}

	qrChan, err := c.WA.GetQRChannel(ctx)
	if err != nil {
		log.Error().Err(err).Msg("get qr channel")
		return 2
	}
	if err := c.WA.Connect(); err != nil {
		log.Error().Err(err).Msg("connect")
		return 2
	}

	fmt.Println()
	fmt.Println("Open WhatsApp on the phone that owns the target account:")
	fmt.Println("  Settings -> Linked Devices -> Link a Device")
	fmt.Println("Scan the QR code below. A fresh code is redrawn until pairing completes.")
	fmt.Println()

	if err := waitForPairing(ctx, qrChan); err != nil {
		log.Error().Err(err).Msg("pairing failed")
		return 2
	}
	log.Info().
		Str("jid", c.WA.Store.ID.String()).
		Dur("grace", postPairGrace).
		Msg("pair-success received; keeping connection open for post-pair handshake")

	graceCtx, graceCancel := context.WithTimeout(ctx, postPairGrace)
	defer graceCancel()
	<-graceCtx.Done()
	log.Info().Msg("pairing complete — session stored, disconnecting")
	return 0
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
