package wa

import (
	"fmt"

	"github.com/rs/zerolog"
	waLog "go.mau.fi/whatsmeow/util/log"
)

// waLogger bridges whatsmeow's waLog.Logger interface onto a zerolog.Logger
// so all output funnels through our single structured logger.
type waLogger struct {
	zl     zerolog.Logger
	module string
}

func NewWaLog(zl zerolog.Logger, module string) waLog.Logger {
	return &waLogger{zl: zl.With().Str("mod", module).Logger(), module: module}
}

func (w *waLogger) Warnf(msg string, args ...interface{}) {
	w.zl.Warn().Msg(fmt.Sprintf(msg, args...))
}
func (w *waLogger) Errorf(msg string, args ...interface{}) {
	w.zl.Error().Msg(fmt.Sprintf(msg, args...))
}
func (w *waLogger) Infof(msg string, args ...interface{}) {
	w.zl.Info().Msg(fmt.Sprintf(msg, args...))
}
func (w *waLogger) Debugf(msg string, args ...interface{}) {
	w.zl.Debug().Msg(fmt.Sprintf(msg, args...))
}
func (w *waLogger) Sub(module string) waLog.Logger {
	sub := w.module + "/" + module
	return &waLogger{zl: w.zl.With().Str("mod", sub).Logger(), module: sub}
}
