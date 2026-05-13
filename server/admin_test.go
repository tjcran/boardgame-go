package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tjcran/boardgame-go/games/tictactoe"
	"github.com/tjcran/boardgame-go/match"
	"github.com/tjcran/boardgame-go/storage"
)

func TestAdminMuxServesPprofAndVars(t *testing.T) {
	srv := httptest.NewServer(AdminMux())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/debug/vars")
	if err != nil {
		t.Fatalf("vars: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), "boardgame_go") {
		t.Fatalf("expected boardgame_go counters in /debug/vars, got: %s", string(body))
	}

	resp, err = http.Get(srv.URL + "/debug/pprof/")
	if err != nil {
		t.Fatalf("pprof: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200 from pprof, got %d", resp.StatusCode)
	}
}

func TestMetricsCountersBumpOnEvents(t *testing.T) {
	start := metrics.MatchesCreated.Load()

	m := match.NewManager(storage.NewMemory())
	m.MustRegister(tictactoe.New())
	srv := httptest.NewServer(New(m))
	defer srv.Close()

	postJSON(t, srv.URL+"/games/tic-tac-toe/create", nil)
	postJSON(t, srv.URL+"/games/tic-tac-toe/create", nil)
	if got := metrics.MatchesCreated.Load() - start; got != 2 {
		t.Fatalf("expected 2 creates counted, got %d", got)
	}
}
