package logging

import (
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

// New returns a zerolog.Logger with console output on stderr. Unknown level
// strings fall back to info.
func New(level string) zerolog.Logger {
	lvl, err := zerolog.ParseLevel(strings.ToLower(strings.TrimSpace(level)))
	if err != nil || level == "" {
		lvl = zerolog.InfoLevel
	}
	zerolog.TimeFieldFormat = time.RFC3339
	out := zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339}
	return zerolog.New(out).Level(lvl).With().Timestamp().Logger()
}
