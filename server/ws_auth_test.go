package server

import (
	"context"
	"net/http"
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

// newAuthFixture spins up a tic-tac-toe match with one seated player
// and returns the server URL, match ID, and the seated player's
// credentials. Used by every WS-auth test in this file.
func newAuthFixture(t *testing.T) (srvURL, matchID string, alice joinResp, srv *httptest.Server) {
	t.Helper()
	m := match.NewManager(storage.NewMemory())
	m.MustRegister(tictactoe.New())
	srv = httptest.NewServer(New(m))

	var created struct{ MatchID string }
	decode(t, postJSON(t, srv.URL+"/games/tic-tac-toe/create", nil), &created)
	alice = joinByHTTP(t, srv.URL, "tic-tac-toe", created.MatchID, "alice")
	return srv.URL, created.MatchID, alice, srv
}

func wsURL(srvURL, matchID, query string) string {
	u := strings.Replace(srvURL, "http://", "ws://", 1) +
		"/games/tic-tac-toe/" + matchID + "/ws"
	if query != "" {
		u += "?" + query
	}
	return u
}

// TestWSRejectsSeatWithoutCredentials guards H2: opening a player-bound
// socket without credentials must not leak that seat's PlayerView.
func TestWSRejectsSeatWithoutCredentials(t *testing.T) {
	srvURL, matchID, alice, srv := newAuthFixture(t)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, resp, err := websocket.Dial(ctx, wsURL(srvURL, matchID, "playerID="+alice.PlayerID), nil)
	if err == nil {
		t.Fatal("expected dial failure when credentials missing")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		gotStatus := 0
		if resp != nil {
			gotStatus = resp.StatusCode
		}
		t.Fatalf("expected HTTP 401, got %d (%v)", gotStatus, err)
	}
}

// TestWSRejectsWrongCredentials guards H2: credentials must match the
// claimed seat's stored token. Any other value is rejected.
func TestWSRejectsWrongCredentials(t *testing.T) {
	srvURL, matchID, alice, srv := newAuthFixture(t)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, resp, err := websocket.Dial(ctx, wsURL(srvURL, matchID,
		"playerID="+alice.PlayerID+"&credentials=wrong"), nil)
	if err == nil {
		t.Fatal("expected dial failure with wrong credentials")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		gotStatus := 0
		if resp != nil {
			gotStatus = resp.StatusCode
		}
		t.Fatalf("expected HTTP 401, got %d (%v)", gotStatus, err)
	}
}

// TestWSSpectatorNeedsNoCredentials confirms the auth tightening
// didn't break spectator (unseated) sockets.
func TestWSSpectatorNeedsNoCredentials(t *testing.T) {
	srvURL, matchID, _, srv := newAuthFixture(t)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL(srvURL, matchID, ""), nil)
	if err != nil {
		t.Fatalf("spectator dial: %v", err)
	}
	defer conn.CloseNow()

	// Should receive the spectator-view sync frame.
	var sync map[string]any
	if err := wsjson.Read(ctx, conn, &sync); err != nil {
		t.Fatalf("read sync: %v", err)
	}
	if sync["type"] != "sync" {
		t.Fatalf("expected sync frame, got %v", sync["type"])
	}
}

// TestWSRejectsForeignOrigin guards H2's cross-site-WebSocket-hijacking
// vector. With a configured Origins allow-list, a foreign Origin must
// be refused before the upgrade.
func TestWSRejectsForeignOrigin(t *testing.T) {
	m := match.NewManager(storage.NewMemory())
	m.MustRegister(tictactoe.New())
	s := New(m)
	s.Origins = []string{"https://app.example.com"}
	srv := httptest.NewServer(s)
	defer srv.Close()

	var created struct{ MatchID string }
	decode(t, postJSON(t, srv.URL+"/games/tic-tac-toe/create", nil), &created)
	joinByHTTP(t, srv.URL, "tic-tac-toe", created.MatchID, "alice")

	hdr := http.Header{}
	hdr.Set("Origin", "https://evil.com")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, resp, err := websocket.Dial(ctx, wsURL(srv.URL, created.MatchID, ""),
		&websocket.DialOptions{HTTPHeader: hdr})
	if err == nil {
		t.Fatal("expected dial failure for foreign Origin")
	}
	if resp == nil || resp.StatusCode != http.StatusForbidden {
		gotStatus := 0
		if resp != nil {
			gotStatus = resp.StatusCode
		}
		t.Fatalf("expected HTTP 403, got %d (%v)", gotStatus, err)
	}
}

// TestWSChatRejectsImpersonation guards the chat-impersonation path:
// the connection's authenticated playerID is alice's, but a chat
// frame claims to be bob with bob-shaped credentials that don't
// match. The server must reject the chat without forwarding.
func TestWSChatRejectsImpersonation(t *testing.T) {
	m := match.NewManager(storage.NewMemory())
	m.MustRegister(tictactoe.New())
	srv := httptest.NewServer(New(m))
	defer srv.Close()

	var created struct{ MatchID string }
	decode(t, postJSON(t, srv.URL+"/games/tic-tac-toe/create", nil), &created)
	alice := joinByHTTP(t, srv.URL, "tic-tac-toe", created.MatchID, "alice")
	bob := joinByHTTP(t, srv.URL, "tic-tac-toe", created.MatchID, "bob")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx,
		wsURL(srv.URL, created.MatchID,
			"playerID="+alice.PlayerID+"&credentials="+alice.PlayerCredentials), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.CloseNow()

	// Drain the initial sync + matchData frames.
	for i := 0; i < 2; i++ {
		var frame map[string]any
		if err := wsjson.Read(ctx, conn, &frame); err != nil {
			t.Fatalf("drain: %v", err)
		}
	}

	// Send a chat frame claiming to be bob, but with bogus credentials.
	if err := wsjson.Write(ctx, conn, map[string]any{
		"type":        "chat",
		"playerID":    bob.PlayerID,
		"credentials": "wrong",
		"payload":     "I'm bob, trust me",
	}); err != nil {
		t.Fatalf("write chat: %v", err)
	}

	// Server should respond with an error frame, not forward to chat
	// subscribers. Read with a short deadline; the response should be
	// the error.
	readCtx, readCancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer readCancel()
	var frame map[string]any
	if err := wsjson.Read(readCtx, conn, &frame); err != nil {
		t.Fatalf("read response: %v", err)
	}
	if frame["type"] != "error" {
		t.Fatalf("expected error frame for impersonated chat, got %v", frame)
	}
}
