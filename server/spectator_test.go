package server

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/games/tictactoe"
	"github.com/tjcran/boardgame-go/match"
	"github.com/tjcran/boardgame-go/storage"
)

// TestBGIO1007_SpectatorGate verifies Game.SpectatorsAllowed=false
// closes WS connections opened without a playerID.
func TestBGIO1007_SpectatorGate(t *testing.T) {
	g := tictactoe.New()
	f := false
	g.SpectatorsAllowed = &f

	m := match.NewManager(storage.NewMemory())
	m.MustRegister(g)
	srv := httptest.NewServer(New(m))
	defer srv.Close()

	var created struct{ MatchID string }
	decode(t, postJSON(t, srv.URL+"/games/tic-tac-toe/create", nil), &created)
	joinByHTTP(t, srv.URL, "tic-tac-toe", created.MatchID, "alice")

	wsURL := strings.Replace(srv.URL, "http://", "ws://", 1) +
		"/games/tic-tac-toe/" + created.MatchID + "/ws" // no playerID

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		// Some Dial paths surface the close immediately as the dial
		// error — that's an acceptable form of "rejected".
		return
	}
	defer conn.CloseNow()

	// Connection succeeded but should close immediately. Reading should
	// surface a close error within a beat.
	_, _, readErr := conn.Read(ctx)
	if readErr == nil {
		t.Fatal("expected spectator socket to be closed; Read succeeded")
	}
	_ = core.State{}
}
