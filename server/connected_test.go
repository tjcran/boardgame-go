package server

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/tjcran/boardgame-go/games/tictactoe"
	"github.com/tjcran/boardgame-go/match"
	"github.com/tjcran/boardgame-go/storage"
)

// TestWSConnectFlipsIsConnected verifies that opening a player-bound
// socket flips IsConnected=true and closing flips it back to false, and
// that each transition produces a matchData broadcast.
func TestWSConnectFlipsIsConnected(t *testing.T) {
	m := match.NewManager(storage.NewMemory())
	m.Register(tictactoe.New())
	srv := httptest.NewServer(New(m))
	defer srv.Close()

	var created struct{ MatchID string }
	decode(t, postJSON(t, srv.URL+"/games/tic-tac-toe/create", nil), &created)
	alice := joinByHTTP(t, srv.URL, "tic-tac-toe", created.MatchID, "alice")

	wsURL := strings.Replace(srv.URL, "http://", "ws://", 1) +
		"/games/tic-tac-toe/" + created.MatchID +
		"/ws?playerID=" + alice.PlayerID +
		"&credentials=" + alice.PlayerCredentials

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	// Drain sync.
	var sync map[string]any
	if err := wsjson.Read(ctx, conn, &sync); err != nil {
		t.Fatalf("sync: %v", err)
	}

	// Server should have flipped IsConnected to true and emitted a
	// matchData frame.
	var md map[string]any
	if err := wsjson.Read(ctx, conn, &md); err != nil {
		t.Fatalf("matchData: %v", err)
	}
	if md["type"] != "matchData" {
		t.Fatalf("expected matchData frame, got %v", md["type"])
	}
	players := md["matchData"].([]any)
	if !players[0].(map[string]any)["isConnected"].(bool) {
		t.Fatalf("expected alice isConnected=true, got %+v", players[0])
	}

	// Close the socket and verify the manager flipped it back off by
	// peeking at storage. Use a short wait because SetConnected runs in
	// the deferred close path on the server side.
	conn.Close(websocket.StatusNormalClosure, "")

	deadline := time.Now().Add(2 * time.Second)
	for {
		mm, _ := m.State(created.MatchID)
		if !mm.Players[0].IsConnected {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected isConnected=false after disconnect, got %+v", mm.Players[0])
		}
		time.Sleep(20 * time.Millisecond)
	}
}
