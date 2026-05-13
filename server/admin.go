package server

import (
	"expvar"
	"net/http"
	"net/http/pprof"
	"sync/atomic"
)

// Metrics holds the process-wide counters this package exposes via expvar.
// Mounting AdminMux registers them automatically.
type Metrics struct {
	MatchesCreated  atomic.Int64
	MatchesJoined   atomic.Int64
	MovesApplied    atomic.Int64
	MovesRejected   atomic.Int64
	ChatMessages    atomic.Int64
	WebSocketConns  atomic.Int64
}

// metrics is the package-global metrics struct. It's wired up by the server
// at request time (handlers bump counters). Exposed as `boardgame_go` in
// `/debug/vars`.
var metrics = &Metrics{}

func init() {
	expvar.Publish("boardgame_go", expvar.Func(func() any {
		return map[string]int64{
			"matches_created":  metrics.MatchesCreated.Load(),
			"matches_joined":   metrics.MatchesJoined.Load(),
			"moves_applied":    metrics.MovesApplied.Load(),
			"moves_rejected":   metrics.MovesRejected.Load(),
			"chat_messages":    metrics.ChatMessages.Load(),
			"websocket_conns":  metrics.WebSocketConns.Load(),
		}
	}))
}

// AdminMux returns an http.Handler that serves /debug/pprof/* and
// /debug/vars. Mount it on a separate port or behind an auth layer — these
// endpoints leak heap profiles, goroutine dumps and CPU traces.
//
// Typical usage:
//
//	go http.ListenAndServe("127.0.0.1:6060", server.AdminMux())
func AdminMux() http.Handler {
	mux := http.NewServeMux()
	// expvar default handler at /debug/vars
	mux.Handle("/debug/vars", expvar.Handler())
	// pprof: this is the same wiring pprof.init() uses on
	// http.DefaultServeMux, but mounted on our own mux so it isn't
	// inadvertently exposed on the main public port.
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	return mux
}
