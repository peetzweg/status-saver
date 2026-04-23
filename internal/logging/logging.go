package logging

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

// New returns a zerolog.Logger configured with console output (human-readable
// on stderr). The level string is parsed loosely; unknown values fall back to info.
func New(level string) zerolog.Logger {
	lvl, err := zerolog.ParseLevel(strings.ToLower(strings.TrimSpace(level)))
	if err != nil || level == "" {
		lvl = zerolog.InfoLevel
	}
	zerolog.TimeFieldFormat = time.RFC3339
	out := zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339}
	return zerolog.New(out).Level(lvl).With().Timestamp().Logger()
}

// WaLogAdapter adapts zerolog.Logger to the waLog.Logger interface used by
// whatsmeow (Warnf/Errorf/Infof/Debugf + Sub). Defined here as its own type
// rather than importing waLog so the logging package stays dependency-light.
type WaLogAdapter struct {
	zl     zerolog.Logger
	module string
}

func NewWaLogAdapter(zl zerolog.Logger, module string) *WaLogAdapter {
	return &WaLogAdapter{zl: zl.With().Str("mod", module).Logger(), module: module}
}

func (w *WaLogAdapter) Warnf(msg string, args ...interface{}) {
	w.zl.Warn().Msg(fmt.Sprintf(msg, args...))
}
func (w *WaLogAdapter) Errorf(msg string, args ...interface{}) {
	w.zl.Error().Msg(fmt.Sprintf(msg, args...))
}
func (w *WaLogAdapter) Infof(msg string, args ...interface{}) {
	w.zl.Info().Msg(fmt.Sprintf(msg, args...))
}
func (w *WaLogAdapter) Debugf(msg string, args ...interface{}) {
	w.zl.Debug().Msg(fmt.Sprintf(msg, args...))
}
func (w *WaLogAdapter) Sub(module string) *WaLogAdapter {
	sub := w.module
	if sub == "" {
		sub = module
	} else {
		sub = sub + "/" + module
	}
	return &WaLogAdapter{zl: w.zl.With().Str("mod", sub).Logger(), module: sub}
}
