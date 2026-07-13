package server

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"net/http/httptest"

	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/match"
	"github.com/tjcran/boardgame-go/storage"
)

// blockGameState is the G payload for the mini block-resume game below.
type blockGameState struct {
	Picked string `json:"picked"`
}

// newBlockGame returns a minimal game exercising the interactive-block
// flow over transports: "ask" queues a block tagged "pick" for the
// moving player; "pick" is the resume move that records its argument.
// Turn limits are left open so both moves fit in one turn.
func newBlockGame() *core.Game {
	return &core.Game{
		Name:       "block-game",
		MinPlayers: 2,
		MaxPlayers: 2,
		Setup:      func(_ core.Ctx, _ any) core.G { return &blockGameState{} },
		DecodeG: func(raw json.RawMessage) (core.G, error) {
			var s blockGameState
			if err := json.Unmarshal(raw, &s); err != nil {
				return nil, err
			}
			return &s, nil
		},
		Moves: map[string]any{
			"ask": core.MoveFn(func(mc *core.MoveContext, _ ...any) (core.G, error) {
				mc.Queue.Block("pick", mc.PlayerID, nil)
				return mc.G, nil
			}),
			"pick": core.MoveFn(func(mc *core.MoveContext, args ...any) (core.G, error) {
				s := mc.G.(*blockGameState)
				if len(args) > 0 {
					if v, ok := args[0].(string); ok {
						s.Picked = v
					}
				}
				return s, nil
			}),
		},
		Turn: &core.TurnConfig{MinMoves: 0, MaxMoves: 10},
	}
}

func newBlockFixture(t *testing.T) (srvURL, matchID string, seat0 joinResp, srv *httptest.Server) {
	t.Helper()
	m := match.NewManager(storage.NewMemory())
	m.MustRegister(newBlockGame())
	srv = httptest.NewServer(New(m))

	var created struct{ MatchID string }
	decode(t, postJSON(t, srv.URL+"/games/block-game/create", nil), &created)
	seat0 = joinByHTTP(t, srv.URL, "block-game", created.MatchID, "alice")
	// Seat the second player too — a MinPlayers=2 match rejects moves
	// until every seat is filled.
	_ = joinByHTTP(t, srv.URL, "block-game", created.MatchID, "bob")
	return srv.URL, created.MatchID, seat0, srv
}

// TestWSMoveResumeTag proves the WS envelope's resumeTag reaches the
// reducer: a blocked match rejects a tagless move with ErrBlocked, and
// the same move with resumeTag set clears the block and applies.
func TestWSMoveResumeTag(t *testing.T) {
	srvURL, matchID, seat0, srv := newBlockFixture(t)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	u := wsURLFor(srvURL, "block-game", matchID,
		"playerID="+seat0.PlayerID+"&credentials="+seat0.PlayerCredentials)
	conn, _, err := websocket.Dial(ctx, u, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "done")
	conn.SetReadLimit(1 << 20)

	// Initial sync frame.
	var frame map[string]any
	if err := wsjson.Read(ctx, conn, &frame); err != nil {
		t.Fatalf("read sync: %v", err)
	}

	send := func(msg map[string]any) {
		t.Helper()
		if err := wsjson.Write(ctx, conn, msg); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	// read skips broadcast noise (matchData/chat) and returns the next
	// update or error frame.
	read := func() map[string]any {
		t.Helper()
		for {
			var f map[string]any
			if err := wsjson.Read(ctx, conn, &f); err != nil {
				t.Fatalf("read: %v", err)
			}
			switch f["type"] {
			case "update", "error", "sync":
				return f
			}
		}
	}

	// 1. "ask" queues the block.
	send(map[string]any{
		"type": "move", "playerID": seat0.PlayerID,
		"credentials": seat0.PlayerCredentials, "move": "ask", "args": []any{},
	})
	f := read()
	if f["type"] != "update" {
		t.Fatalf("ask: want update frame, got %v", f)
	}

	// 2. Tagless "pick" is rejected while the block is pending.
	send(map[string]any{
		"type": "move", "playerID": seat0.PlayerID,
		"credentials": seat0.PlayerCredentials, "move": "pick", "args": []any{"nope"},
	})
	f = read()
	if f["type"] != "error" {
		t.Fatalf("tagless pick: want error frame (ErrBlocked), got %v", f)
	}

	// 3. "pick" with resumeTag clears the block and applies.
	send(map[string]any{
		"type": "move", "playerID": seat0.PlayerID,
		"credentials": seat0.PlayerCredentials, "move": "pick",
		"args": []any{"chosen"}, "resumeTag": "pick",
	})
	f = read()
	if f["type"] != "update" {
		t.Fatalf("tagged pick: want update frame, got %v", f)
	}
	state, _ := f["state"].(map[string]any)
	g, _ := state["G"].(map[string]any)
	if g["picked"] != "chosen" {
		t.Fatalf("resume move did not apply: G=%v", g)
	}
}

// TestHTTPMoveResumeTag proves the REST /move body's resumeTag reaches
// the reducer the same way.
func TestHTTPMoveResumeTag(t *testing.T) {
	srvURL, matchID, seat0, srv := newBlockFixture(t)
	defer srv.Close()

	moveURL := srvURL + "/games/block-game/" + matchID + "/move"
	// Queue the block.
	resp := postJSON(t, moveURL, map[string]any{
		"playerID": seat0.PlayerID, "credentials": seat0.PlayerCredentials,
		"move": "ask", "args": []any{},
	})
	if resp.StatusCode != 200 {
		t.Fatalf("ask: HTTP %d", resp.StatusCode)
	}
	// Tagless move rejected.
	resp = postJSON(t, moveURL, map[string]any{
		"playerID": seat0.PlayerID, "credentials": seat0.PlayerCredentials,
		"move": "pick", "args": []any{"nope"},
	})
	if resp.StatusCode == 200 {
		t.Fatal("tagless pick: want rejection while block pending, got 200")
	}
	// Tagged move applies.
	resp = postJSON(t, moveURL, map[string]any{
		"playerID": seat0.PlayerID, "credentials": seat0.PlayerCredentials,
		"move": "pick", "args": []any{"chosen"}, "resumeTag": "pick",
	})
	if resp.StatusCode != 200 {
		t.Fatalf("tagged pick: HTTP %d", resp.StatusCode)
	}
	var state struct {
		G blockGameState `json:"G"`
	}
	decode(t, resp, &state)
	if state.G.Picked != "chosen" {
		t.Fatalf("resume move did not apply: picked=%q", state.G.Picked)
	}
}

// wsURLFor mirrors ws_auth_test.go's wsURL but for an arbitrary game.
func wsURLFor(srvURL, game, matchID, query string) string {
	u := srvURL
	u = "ws" + u[len("http"):]
	u += "/games/" + game + "/" + matchID + "/ws"
	if query != "" {
		u += "?" + query
	}
	return u
}
