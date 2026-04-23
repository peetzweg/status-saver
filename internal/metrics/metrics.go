// Package metrics exposes a tiny Prometheus-text-format /metrics endpoint
// and a /health probe for the daemon. Deliberately stdlib-only — adding
// prometheus/client_golang would be overkill for the handful of counters
// we actually want.
package metrics

import (
	"fmt"
	"net/http"
	"sync/atomic"
	"time"
)

// Recorder holds the atomic counters. Safe for concurrent use.
type Recorder struct {
	archived         atomic.Uint64
	errors           atomic.Uint64
	lastArchivedUnix atomic.Int64
	started          time.Time
}

func New() *Recorder {
	return &Recorder{started: time.Now()}
}

// RecordArchived bumps the archived counter and updates the
// last-archived timestamp. Called from the status handler after a
// successful write + index insert.
func (r *Recorder) RecordArchived() {
	r.archived.Add(1)
	r.lastArchivedUnix.Store(time.Now().Unix())
}

// RecordError bumps the error counter. Called from the status handler on
// any download/write/index error.
func (r *Recorder) RecordError() {
	r.errors.Add(1)
}

// Handler returns an http.Handler serving /health and /metrics. `connected`
// is a callback that reports whether the daemon currently holds a live
// WhatsApp WebSocket (typically whatsmeow.Client.IsConnected).
func (r *Recorder) Handler(connected func() bool) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		if connected() {
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprintln(w, "ok")
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = fmt.Fprintln(w, "not connected")
	})
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		conn := 0
		if connected() {
			conn = 1
		}
		uptime := int64(time.Since(r.started).Seconds())
		_, _ = fmt.Fprintf(w,
			"# HELP statussaver_archived_total Total status posts archived to disk.\n"+
				"# TYPE statussaver_archived_total counter\n"+
				"statussaver_archived_total %d\n"+
				"# HELP statussaver_errors_total Total handler errors (download/write/index failures).\n"+
				"# TYPE statussaver_errors_total counter\n"+
				"statussaver_errors_total %d\n"+
				"# HELP statussaver_connected Whether the WhatsApp session is connected (1) or not (0).\n"+
				"# TYPE statussaver_connected gauge\n"+
				"statussaver_connected %d\n"+
				"# HELP statussaver_last_archived_timestamp_seconds Unix time of the most recent successful archive.\n"+
				"# TYPE statussaver_last_archived_timestamp_seconds gauge\n"+
				"statussaver_last_archived_timestamp_seconds %d\n"+
				"# HELP statussaver_uptime_seconds Seconds since the daemon started.\n"+
				"# TYPE statussaver_uptime_seconds gauge\n"+
				"statussaver_uptime_seconds %d\n",
			r.archived.Load(),
			r.errors.Load(),
			conn,
			r.lastArchivedUnix.Load(),
			uptime,
		)
	})
	return mux
}
