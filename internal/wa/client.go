package wa

import (
	"context"
	"fmt"
	"sync"

	"github.com/rs/zerolog"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types/events"

	_ "github.com/mattn/go-sqlite3"
)

// Client wraps a whatsmeow.Client together with its session container and a
// LoggedOut channel that is closed when the server forcibly ends the session
// (user removed the device from their phone, ban, etc.). Callers treat a
// closed LoggedOut channel as a signal to exit and re-pair.
type Client struct {
	WA        *whatsmeow.Client
	Container *sqlstore.Container
	Log       zerolog.Logger

	LoggedOut chan struct{}
	once      sync.Once
}

// Open prepares the session store and creates (but does not connect) the
// whatsmeow client. If the store has no paired device, the returned client's
// WA.Store.ID is nil — the pairing binary detects this and runs QR flow.
func Open(ctx context.Context, sessionDB string, log zerolog.Logger) (*Client, error) {
	dbLog := NewWaLog(log, "wa-db")
	dsn := "file:" + sessionDB + "?_foreign_keys=on&_journal=WAL&_busy_timeout=5000"
	container, err := sqlstore.New(ctx, "sqlite3", dsn, dbLog)
	if err != nil {
		return nil, fmt.Errorf("open sqlstore %s: %w", sessionDB, err)
	}
	device, err := container.GetFirstDevice(ctx)
	if err != nil {
		return nil, fmt.Errorf("get first device: %w", err)
	}
	waClient := whatsmeow.NewClient(device, NewWaLog(log, "wa"))

	c := &Client{
		WA:        waClient,
		Container: container,
		Log:       log,
		LoggedOut: make(chan struct{}),
	}
	waClient.AddEventHandler(c.dispatch)
	return c, nil
}

// IsPaired reports whether the device store already contains a session.
func (c *Client) IsPaired() bool {
	return c.WA.Store.ID != nil
}

// Close disconnects the client and closes the underlying session container.
func (c *Client) Close() {
	if c.WA != nil && c.WA.IsConnected() {
		c.WA.Disconnect()
	}
	if c.Container != nil {
		_ = c.Container.Close()
	}
}

// extraHandler is an optional second-stage event handler installed by the
// daemon to process messages. Kept here so the client package owns the
// event-dispatch entry point and the status package stays focused on logic.
type extraHandler func(evt interface{})

var extra extraHandler

// SetMessageHandler registers the function that will receive every raw event
// from whatsmeow (after we've processed connection lifecycle events). Called
// once on startup by the daemon.
func (c *Client) SetMessageHandler(h extraHandler) { extra = h }

func (c *Client) dispatch(evt interface{}) {
	switch v := evt.(type) {
	case *events.Connected:
		c.Log.Info().Msg("whatsapp connected")
	case *events.Disconnected:
		c.Log.Warn().Msg("whatsapp disconnected (will auto-reconnect)")
	case *events.LoggedOut:
		c.Log.Error().Str("reason", v.Reason.String()).Msg("whatsapp logged out — session invalid")
		c.once.Do(func() { close(c.LoggedOut) })
	case *events.KeepAliveTimeout:
		c.Log.Warn().Msg("keepalive timeout")
	case *events.KeepAliveRestored:
		c.Log.Info().Msg("keepalive restored")
	case *events.TemporaryBan:
		c.Log.Error().Str("ban", v.String()).Msg("temporary ban from server")
	case *events.OfflineSyncPreview:
		c.Log.Info().
			Int("total", v.Total).
			Int("messages", v.Messages).
			Int("notifications", v.Notifications).
			Int("receipts", v.Receipts).
			Msg("server will replay events missed during downtime")
	case *events.OfflineSyncCompleted:
		c.Log.Info().Int("count", v.Count).Msg("offline sync finished")
	}
	if extra != nil {
		extra(evt)
	}
}
