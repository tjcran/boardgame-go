package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
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

// testServer brings up a real http.Server with the tic-tac-toe game wired in.
// We use httptest so handlers run under realistic conditions (including the
// WebSocket upgrade).
func testServer(t *testing.T) (*httptest.Server, *match.Manager) {
	t.Helper()
	m := match.NewManager(storage.NewMemory())
	m.Register(tictactoe.New())
	s := httptest.NewServer(New(m))
	t.Cleanup(s.Close)
	return s, m
}

func postJSON(t *testing.T, url string, body any) *http.Response {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req, _ := http.NewRequest(http.MethodPost, url, &buf)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post %s: %v", url, err)
	}
	return resp
}

func decode(t *testing.T, r *http.Response, v any) {
	t.Helper()
	b, _ := io.ReadAll(r.Body)
	r.Body.Close()
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatalf("decode (%d %s): %v", r.StatusCode, string(b), err)
	}
}

func TestCreateJoinMoveFlow(t *testing.T) {
	srv, _ := testServer(t)

	var created struct{ MatchID string }
	decode(t, postJSON(t, srv.URL+"/games/tic-tac-toe/create", nil), &created)
	if created.MatchID == "" {
		t.Fatal("no match id")
	}

	var alice, bob struct {
		PlayerID string `json:"playerID"`
		Seat     string `json:"seat"`
	}
	decode(t, postJSON(t, srv.URL+"/games/tic-tac-toe/"+created.MatchID+"/join",
		map[string]string{"name": "alice"}), &alice)
	decode(t, postJSON(t, srv.URL+"/games/tic-tac-toe/"+created.MatchID+"/join",
		map[string]string{"name": "bob"}), &bob)
	if alice.Seat != "0" || bob.Seat != "1" {
		t.Fatalf("seats: alice=%s bob=%s", alice.Seat, bob.Seat)
	}

	// alice plays cell 4
	resp := postJSON(t, srv.URL+"/games/tic-tac-toe/"+created.MatchID+"/move",
		map[string]any{"playerID": alice.PlayerID, "move": "clickCell", "args": []any{4}})
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("move: %d %s", resp.StatusCode, string(body))
	}

	// bob playing out of turn: should fail
	resp = postJSON(t, srv.URL+"/games/tic-tac-toe/"+created.MatchID+"/move",
		map[string]any{"playerID": alice.PlayerID, "move": "clickCell", "args": []any{0}})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", resp.StatusCode)
	}
}

func TestUnknownGameReturns404(t *testing.T) {
	srv, _ := testServer(t)
	resp := postJSON(t, srv.URL+"/games/missing/create", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestWebSocketReceivesStateUpdate(t *testing.T) {
	srv, _ := testServer(t)

	var created struct{ MatchID string }
	decode(t, postJSON(t, srv.URL+"/games/tic-tac-toe/create", nil), &created)

	var alice, bob struct {
		PlayerID string `json:"playerID"`
		Seat     string `json:"seat"`
	}
	decode(t, postJSON(t, srv.URL+"/games/tic-tac-toe/"+created.MatchID+"/join",
		map[string]string{"name": "alice"}), &alice)
	decode(t, postJSON(t, srv.URL+"/games/tic-tac-toe/"+created.MatchID+"/join",
		map[string]string{"name": "bob"}), &bob)

	wsURL := strings.Replace(srv.URL, "http://", "ws://", 1) +
		"/games/tic-tac-toe/" + created.MatchID + "/ws"

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.CloseNow()

	// First frame is the current state on connect.
	var initial map[string]any
	if err := wsjson.Read(ctx, conn, &initial); err != nil {
		t.Fatalf("initial read: %v", err)
	}
	if initial["type"] != "state" {
		t.Fatalf("expected state frame, got %v", initial)
	}

	// Submit a move through HTTP and expect the WS to push the new state.
	resp := postJSON(t, srv.URL+"/games/tic-tac-toe/"+created.MatchID+"/move",
		map[string]any{"playerID": alice.PlayerID, "move": "clickCell", "args": []any{0}})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("move: %d", resp.StatusCode)
	}

	var pushed map[string]any
	if err := wsjson.Read(ctx, conn, &pushed); err != nil {
		t.Fatalf("ws read: %v", err)
	}
	if pushed["type"] != "state" {
		t.Fatalf("expected state push, got %v", pushed)
	}
}
