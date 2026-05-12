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

func getJSON(t *testing.T, url string) *http.Response {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("get %s: %v", url, err)
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

// joinResp is the typed shape of the join endpoint's response.
type joinResp struct {
	PlayerID          string `json:"playerID"`
	Seat              string `json:"seat"`
	PlayerCredentials string `json:"playerCredentials"`
}

// joinByHTTP joins via POST and returns the response.
func joinByHTTP(t *testing.T, url, gameName, matchID, playerName string) joinResp {
	t.Helper()
	var jr joinResp
	decode(t, postJSON(t, url+"/games/"+gameName+"/"+matchID+"/join",
		map[string]any{"playerName": playerName}), &jr)
	return jr
}

func TestCreateJoinMoveFlow(t *testing.T) {
	srv, _ := testServer(t)

	var created struct{ MatchID string }
	decode(t, postJSON(t, srv.URL+"/games/tic-tac-toe/create", nil), &created)
	if created.MatchID == "" {
		t.Fatal("no match id")
	}

	alice := joinByHTTP(t, srv.URL, "tic-tac-toe", created.MatchID, "alice")
	bob := joinByHTTP(t, srv.URL, "tic-tac-toe", created.MatchID, "bob")
	if alice.Seat != "0" || bob.Seat != "1" {
		t.Fatalf("seats: alice=%s bob=%s", alice.Seat, bob.Seat)
	}
	if alice.PlayerCredentials == "" {
		t.Fatal("expected credentials issued on join")
	}

	// alice plays cell 4
	resp := postJSON(t, srv.URL+"/games/tic-tac-toe/"+created.MatchID+"/move",
		map[string]any{
			"playerID":    alice.PlayerID,
			"credentials": alice.PlayerCredentials,
			"move":        "clickCell",
			"args":        []any{4},
		})
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("move: %d %s", resp.StatusCode, string(body))
	}

	// alice playing out of turn: should fail
	resp = postJSON(t, srv.URL+"/games/tic-tac-toe/"+created.MatchID+"/move",
		map[string]any{
			"playerID":    alice.PlayerID,
			"credentials": alice.PlayerCredentials,
			"move":        "clickCell",
			"args":        []any{0},
		})
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

func TestListGamesEndpoint(t *testing.T) {
	srv, _ := testServer(t)
	resp := getJSON(t, srv.URL+"/games")
	var names []string
	decode(t, resp, &names)
	if len(names) != 1 || names[0] != "tic-tac-toe" {
		t.Fatalf("expected ['tic-tac-toe'], got %v", names)
	}
}

func TestListMatchesAndGetOne(t *testing.T) {
	srv, _ := testServer(t)
	var created struct{ MatchID string }
	decode(t, postJSON(t, srv.URL+"/games/tic-tac-toe/create", nil), &created)
	joinByHTTP(t, srv.URL, "tic-tac-toe", created.MatchID, "alice")

	// GET /games/tic-tac-toe
	var listed struct {
		Matches []struct {
			MatchID string `json:"matchID"`
			Players []struct {
				ID, Name, Seat string
			} `json:"players"`
		} `json:"matches"`
	}
	decode(t, getJSON(t, srv.URL+"/games/tic-tac-toe"), &listed)
	if len(listed.Matches) != 1 || listed.Matches[0].MatchID != created.MatchID {
		t.Fatalf("listing: %+v", listed)
	}
	if listed.Matches[0].Players[0].Name != "alice" {
		t.Fatalf("alice not in players list: %+v", listed.Matches[0].Players)
	}

	// GET /games/tic-tac-toe/{id}
	var one struct {
		MatchID string `json:"matchID"`
	}
	decode(t, getJSON(t, srv.URL+"/games/tic-tac-toe/"+created.MatchID), &one)
	if one.MatchID != created.MatchID {
		t.Fatalf("get match: %+v", one)
	}
}

func TestLeaveEndpoint(t *testing.T) {
	srv, _ := testServer(t)
	var created struct{ MatchID string }
	decode(t, postJSON(t, srv.URL+"/games/tic-tac-toe/create", nil), &created)
	alice := joinByHTTP(t, srv.URL, "tic-tac-toe", created.MatchID, "alice")

	resp := postJSON(t, srv.URL+"/games/tic-tac-toe/"+created.MatchID+"/leave",
		map[string]any{
			"playerID":    alice.PlayerID,
			"credentials": alice.PlayerCredentials,
		})
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("leave: %d %s", resp.StatusCode, string(body))
	}
}

func TestPlayAgainEndpoint(t *testing.T) {
	srv, _ := testServer(t)
	var created struct{ MatchID string }
	decode(t, postJSON(t, srv.URL+"/games/tic-tac-toe/create", nil), &created)
	alice := joinByHTTP(t, srv.URL, "tic-tac-toe", created.MatchID, "alice")
	joinByHTTP(t, srv.URL, "tic-tac-toe", created.MatchID, "bob")

	var pa struct{ NextMatchID string }
	decode(t, postJSON(t, srv.URL+"/games/tic-tac-toe/"+created.MatchID+"/playAgain",
		map[string]any{
			"playerID":    alice.PlayerID,
			"credentials": alice.PlayerCredentials,
		}), &pa)
	if pa.NextMatchID == "" || pa.NextMatchID == created.MatchID {
		t.Fatalf("expected fresh match id, got %q (orig %q)", pa.NextMatchID, created.MatchID)
	}
}

func TestMoveRejectedWithoutCredentials(t *testing.T) {
	srv, _ := testServer(t)
	var created struct{ MatchID string }
	decode(t, postJSON(t, srv.URL+"/games/tic-tac-toe/create", nil), &created)
	alice := joinByHTTP(t, srv.URL, "tic-tac-toe", created.MatchID, "alice")
	joinByHTTP(t, srv.URL, "tic-tac-toe", created.MatchID, "bob")

	// Send a move with a wrong credential.
	resp := postJSON(t, srv.URL+"/games/tic-tac-toe/"+created.MatchID+"/move",
		map[string]any{
			"playerID":    alice.PlayerID,
			"credentials": "nope",
			"move":        "clickCell",
			"args":        []any{0},
		})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestWebSocketReceivesStateUpdate(t *testing.T) {
	srv, _ := testServer(t)

	var created struct{ MatchID string }
	decode(t, postJSON(t, srv.URL+"/games/tic-tac-toe/create", nil), &created)

	alice := joinByHTTP(t, srv.URL, "tic-tac-toe", created.MatchID, "alice")
	joinByHTTP(t, srv.URL, "tic-tac-toe", created.MatchID, "bob")

	wsURL := strings.Replace(srv.URL, "http://", "ws://", 1) +
		"/games/tic-tac-toe/" + created.MatchID + "/ws"

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.CloseNow()

	// First frame is the BGIO `sync` payload: full state + matchData.
	var initial map[string]any
	if err := wsjson.Read(ctx, conn, &initial); err != nil {
		t.Fatalf("initial read: %v", err)
	}
	if initial["type"] != "sync" {
		t.Fatalf("expected sync frame, got %v", initial)
	}

	// Submit a move through HTTP and expect the WS to push an `update`.
	resp := postJSON(t, srv.URL+"/games/tic-tac-toe/"+created.MatchID+"/move",
		map[string]any{
			"playerID":    alice.PlayerID,
			"credentials": alice.PlayerCredentials,
			"move":        "clickCell",
			"args":        []any{0},
		})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("move: %d", resp.StatusCode)
	}

	var pushed map[string]any
	if err := wsjson.Read(ctx, conn, &pushed); err != nil {
		t.Fatalf("ws read: %v", err)
	}
	if pushed["type"] != "update" {
		t.Fatalf("expected update frame, got %v", pushed)
	}
}

func TestCORSAllowsLocalhostInDevelopment(t *testing.T) {
	m := match.NewManager(storage.NewMemory())
	m.Register(tictactoe.New())
	s := New(m)
	s.Origins = []string{OriginLocalhostInDevelopment}
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/games", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "http://localhost:3000" {
		t.Fatalf("expected CORS allow header for localhost, got %q", got)
	}
}

func TestCORSRejectsUnlistedOrigin(t *testing.T) {
	m := match.NewManager(storage.NewMemory())
	m.Register(tictactoe.New())
	s := New(m)
	s.Origins = []string{"https://allowed.example"}
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/games", nil)
	req.Header.Set("Origin", "https://evil.example")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("expected no CORS allow header for unlisted origin, got %q", got)
	}
}
