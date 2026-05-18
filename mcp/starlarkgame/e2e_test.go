package starlarkgame_test

import (
	"context"
	"testing"

	"github.com/tjcran/boardgame-go/match"
	"github.com/tjcran/boardgame-go/storage"
	"github.com/tjcran/boardgame-go/mcp/starlarkgame"
)

// misereTTT is a misère tic-tac-toe variant: first player to complete a line
// LOSES. Player "0" occupies cells 0,1,2 (top row) → loses → winner is "1".
const misereTTT = `
META = {"name":"misere-ttt","min_players":2,"max_players":2,"description":"three in a row LOSES"}
def setup(ctx): return {"cells": [None]*9}

LINES = [(0,1,2),(3,4,5),(6,7,8),(0,3,6),(1,4,7),(2,5,8),(0,4,8),(2,4,6)]

def _three(state):
    for a,b,c in LINES:
        v = state["cells"][a]
        if v != None and v == state["cells"][b] and v == state["cells"][c]:
            return v
    return None

def _click(state, ctx, idx):
    if state["cells"][idx] != None: fail("occupied")
    new_cells = list(state["cells"])
    new_cells[idx] = ctx.player_id
    return {"cells": new_cells}

MOVES = {"click": {"args":[{"name":"idx","type":"int"}], "apply": _click}}

def end_if(state, ctx):
    v = _three(state)
    if v != None: return {"winner": "1" if v == "0" else "0"}
    if None not in state["cells"]: return {"draw": True}
    return None

def legal_moves(state, ctx):
    return [{"name":"click","args":[i]} for i in range(9) if state["cells"][i] == None]
`

// TestStarlarkGameThroughManager plays out a scripted misère-TTT match end-to-end:
// register spec → create match → join both players → play 5 moves → assert winner="1".
func TestStarlarkGameThroughManager(t *testing.T) {
	spec, err := starlarkgame.LoadSpec(misereTTT)
	if err != nil {
		t.Fatalf("LoadSpec: %v", err)
	}
	if err := starlarkgame.Validate(context.Background(), spec); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	g := starlarkgame.BuildCoreGame(spec)

	mgr := match.NewManager(storage.NewMemory())
	mgr.MustRegister(g)

	matchID, err := mgr.Create("misere-ttt", match.CreateOptions{NumPlayers: 2})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	jr0, err := mgr.Join(matchID, "Alice", match.JoinOptions{PlayerID: "0"})
	if err != nil {
		t.Fatalf("Join player 0: %v", err)
	}
	jr1, err := mgr.Join(matchID, "Bob", match.JoinOptions{PlayerID: "1"})
	if err != nil {
		t.Fatalf("Join player 1: %v", err)
	}

	// Scripted moves: P0 claims top row (0,1,2) → loses → P1 wins.
	// Turn order alternates: P0, P1, P0, P1, P0.
	plays := []struct {
		pid, creds string
		idx        int64
	}{
		{jr0.PlayerID, jr0.PlayerCredentials, 0},
		{jr1.PlayerID, jr1.PlayerCredentials, 3},
		{jr0.PlayerID, jr0.PlayerCredentials, 1},
		{jr1.PlayerID, jr1.PlayerCredentials, 4},
		{jr0.PlayerID, jr0.PlayerCredentials, 2}, // completes top row → P0 loses → winner="1"
	}
	for i, p := range plays {
		if _, err := mgr.Move(matchID, p.pid, p.creds, "click", []any{p.idx}); err != nil {
			t.Fatalf("move %d (player=%s cell=%d): %v", i, p.pid, p.idx, err)
		}
	}

	sm, err := mgr.State(matchID)
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	go_ := sm.State.Ctx.Gameover
	m, ok := go_.(map[string]any)
	if !ok {
		t.Fatalf("expected Gameover to be map[string]any, got %T: %#v", go_, go_)
	}
	if m["winner"] != "1" {
		t.Fatalf("expected winner='1' (P0 lost via misère), got %#v", m)
	}
}
