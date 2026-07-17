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

// redactBlockGameState is the G payload for the mini block-resume game
// below. It reuses the "ask"/"pick" shape from resume_tag_test.go but
// queues a Data payload worth redacting and wires Game.BlockView so the
// payload is only visible to the seat it's addressed to.
type redactBlockGameState struct {
	Picked string `json:"picked"`
}

func newRedactBlockGame() *core.Game {
	return &core.Game{
		Name:       "redact-block-game",
		MinPlayers: 2,
		MaxPlayers: 2,
		Setup:      func(_ core.Ctx, _ any) core.G { return &redactBlockGameState{} },
		DecodeG: func(raw json.RawMessage) (core.G, error) {
			var s redactBlockGameState
			if err := json.Unmarshal(raw, &s); err != nil {
				return nil, err
			}
			return &s, nil
		},
		Moves: map[string]any{
			"ask": core.MoveFn(func(mc *core.MoveContext, _ ...any) (core.G, error) {
				mc.Queue.Block("pick", mc.PlayerID, map[string]any{
					"candidates": []string{"card-7", "card-9"},
				})
				return mc.G, nil
			}),
			"pick": core.MoveFn(func(mc *core.MoveContext, args ...any) (core.G, error) {
				s := mc.G.(*redactBlockGameState)
				if len(args) > 0 {
					if v, ok := args[0].(string); ok {
						s.Picked = v
					}
				}
				return s, nil
			}),
		},
		Turn:      &core.TurnConfig{MinMoves: 0, MaxMoves: 10},
		BlockView: core.HideBlockPayload,
	}
}

func newRedactBlockFixture(t *testing.T) (srvURL, matchID string, seat0, seat1 joinResp, srv *httptest.Server) {
	t.Helper()
	m := match.NewManager(storage.NewMemory())
	m.MustRegister(newRedactBlockGame())
	srv = httptest.NewServer(New(m))

	var created struct{ MatchID string }
	decode(t, postJSON(t, srv.URL+"/games/redact-block-game/create", nil), &created)
	seat0 = joinByHTTP(t, srv.URL, "redact-block-game", created.MatchID, "alice")
	seat1 = joinByHTTP(t, srv.URL, "redact-block-game", created.MatchID, "bob")
	return srv.URL, created.MatchID, seat0, seat1, srv
}

// blockDataOf pulls state.blocks[0].data out of a decoded WS/HTTP frame,
// or nil if there's no block / no data key.
func blockDataOf(state map[string]any) any {
	blocks, _ := state["blocks"].([]any)
	if len(blocks) == 0 {
		return nil
	}
	b, _ := blocks[0].(map[string]any)
	return b["data"]
}

// TestBlockViewRedactsBroadcastPerSeat proves the hook applies at the
// broadcast chokepoint: after "ask" queues a block for seat0, seat0's own
// update frame carries the payload but seat1's simultaneous broadcast
// frame does not — while both still see the block's ownership shell.
func TestBlockViewRedactsBroadcastPerSeat(t *testing.T) {
	srvURL, matchID, seat0, seat1, srv := newRedactBlockFixture(t)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	dial := func(seat joinResp) *websocket.Conn {
		u := wsURLFor(srvURL, "redact-block-game", matchID,
			"playerID="+seat.PlayerID+"&credentials="+seat.PlayerCredentials)
		conn, _, err := websocket.Dial(ctx, u, nil)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		conn.SetReadLimit(1 << 20)
		return conn
	}
	readUpdate := func(conn *websocket.Conn) map[string]any {
		t.Helper()
		for {
			var f map[string]any
			if err := wsjson.Read(ctx, conn, &f); err != nil {
				t.Fatalf("read: %v", err)
			}
			switch f["type"] {
			case "update", "sync":
				return f
			}
		}
	}

	conn0 := dial(seat0)
	defer conn0.Close(websocket.StatusNormalClosure, "done")
	conn1 := dial(seat1)
	defer conn1.Close(websocket.StatusNormalClosure, "done")

	// Drain both initial sync frames (no block queued yet).
	readUpdate(conn0)
	readUpdate(conn1)

	// seat0 queues the block.
	if err := wsjson.Write(ctx, conn0, map[string]any{
		"type": "move", "playerID": seat0.PlayerID,
		"credentials": seat0.PlayerCredentials, "move": "ask", "args": []any{},
	}); err != nil {
		t.Fatalf("write ask: %v", err)
	}

	f0 := readUpdate(conn0)
	f1 := readUpdate(conn1)

	state0, _ := f0["state"].(map[string]any)
	state1, _ := f1["state"].(map[string]any)

	blocks0, _ := state0["blocks"].([]any)
	blocks1, _ := state1["blocks"].([]any)
	if len(blocks0) != 1 || len(blocks1) != 1 {
		t.Fatalf("expected one pending block in both views: seat0=%v seat1=%v", blocks0, blocks1)
	}
	b0 := blocks0[0].(map[string]any)
	b1 := blocks1[0].(map[string]any)

	// Ownership shell survives redaction for both viewers.
	if b0["tag"] != "pick" || b0["playerID"] != seat0.PlayerID {
		t.Fatalf("seat0 shell wrong: %+v", b0)
	}
	if b1["tag"] != "pick" || b1["playerID"] != seat0.PlayerID {
		t.Fatalf("seat1 shell wrong: %+v", b1)
	}

	// The addressed seat sees the payload; the other seat does not.
	if blockDataOf(state0) == nil {
		t.Fatalf("addressed seat should see block payload: %+v", b0)
	}
	if blockDataOf(state1) != nil {
		t.Fatalf("non-addressed seat should NOT see block payload: %+v", b1)
	}

	// Resume still works for the addressed seat: redacting seat1's copy
	// must not perturb the authoritative state the reducer validates
	// against.
	if err := wsjson.Write(ctx, conn0, map[string]any{
		"type": "move", "playerID": seat0.PlayerID,
		"credentials": seat0.PlayerCredentials, "move": "pick",
		"args": []any{"card-7"}, "resumeTag": "pick",
	}); err != nil {
		t.Fatalf("write pick: %v", err)
	}
	f0 = readUpdate(conn0)
	state0, _ = f0["state"].(map[string]any)
	g0, _ := state0["G"].(map[string]any)
	if g0["picked"] != "card-7" {
		t.Fatalf("resume move did not apply after redaction: G=%v", g0)
	}
	blocksAfter, _ := state0["blocks"].([]any)
	if len(blocksAfter) != 0 {
		t.Fatalf("resume should clear the block: %v", blocksAfter)
	}
}

// TestBlockViewAppliesOnReconnectSync proves the hook runs on the initial
// sync frame too (not just broadcast updates) — a seat that queues a block
// and then reconnects (a fresh WS connection, as after a dropped socket)
// must see the same per-seat redaction on the frame the server pushes
// immediately on connect.
func TestBlockViewAppliesOnReconnectSync(t *testing.T) {
	srvURL, matchID, seat0, seat1, srv := newRedactBlockFixture(t)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// seat0 queues the block over plain HTTP so no WS connection needs to
	// be open yet.
	resp := postJSON(t, srvURL+"/games/redact-block-game/"+matchID+"/move", map[string]any{
		"playerID": seat0.PlayerID, "credentials": seat0.PlayerCredentials,
		"move": "ask", "args": []any{},
	})
	if resp.StatusCode != 200 {
		t.Fatalf("ask: HTTP %d", resp.StatusCode)
	}

	// seat1 connects fresh (this is the reconnect/rejoin sync path: the
	// very first frame a client gets when it (re)establishes a socket).
	u := wsURLFor(srvURL, "redact-block-game", matchID,
		"playerID="+seat1.PlayerID+"&credentials="+seat1.PlayerCredentials)
	conn, _, err := websocket.Dial(ctx, u, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "done")
	conn.SetReadLimit(1 << 20)

	var frame map[string]any
	if err := wsjson.Read(ctx, conn, &frame); err != nil {
		t.Fatalf("read sync: %v", err)
	}
	state, _ := frame["state"].(map[string]any)
	blocks, _ := state["blocks"].([]any)
	if len(blocks) != 1 {
		t.Fatalf("expected the pending block to survive reconnect sync: %v", blocks)
	}
	b := blocks[0].(map[string]any)
	if b["tag"] != "pick" || b["playerID"] != seat0.PlayerID {
		t.Fatalf("reconnect sync shell wrong: %+v", b)
	}
	if blockDataOf(state) != nil {
		t.Fatalf("reconnect sync leaked block payload to non-addressed seat: %+v", b)
	}
}

// TestBlockViewAppliesOnHTTPMoveResponse proves the hook also covers the
// REST /move response path (server/http.go re-runs PlayerView for the
// requesting player before writing the response body): the addressed
// seat's own move response carries the full payload.
func TestBlockViewAppliesOnHTTPMoveResponse(t *testing.T) {
	srvURL, matchID, seat0, _, srv := newRedactBlockFixture(t)
	defer srv.Close()

	moveURL := srvURL + "/games/redact-block-game/" + matchID + "/move"
	resp := postJSON(t, moveURL, map[string]any{
		"playerID": seat0.PlayerID, "credentials": seat0.PlayerCredentials,
		"move": "ask", "args": []any{},
	})
	if resp.StatusCode != 200 {
		t.Fatalf("ask: HTTP %d", resp.StatusCode)
	}
	var state map[string]any
	decode(t, resp, &state)
	if blockDataOf(state) == nil {
		t.Fatalf("addressed seat should see block payload on its own HTTP move response: %v", state["blocks"])
	}
}
