package mcp

import (
	"context"
	"testing"

	"github.com/tjcran/boardgame-go/games/tictactoe"
	"github.com/tjcran/boardgame-go/match"
	"github.com/tjcran/boardgame-go/storage"
)

// misereTTTIntegration is a Starlark game spec for misère tic-tac-toe:
// the player who completes three-in-a-row LOSES (opposite of normal rules).
// This serves as a distinct game from the built-in tic-tac-toe so we can
// verify that the register→create→join→move→gameover pipeline is fully
// exercised through the Tools layer.
const misereTTTIntegration = `
META = {"name":"misere-ttt-i","min_players":2,"max_players":2}
LINES = [(0,1,2),(3,4,5),(6,7,8),(0,3,6),(1,4,7),(2,5,8),(0,4,8),(2,4,6)]
def setup(ctx): return {"cells": [None]*9}
def _click(state, ctx, idx):
    if state["cells"][idx] != None: fail("occupied")
    state["cells"][idx] = ctx.player_id
MOVES = {"click": {"args":[{"name":"idx","type":"int"}], "apply": _click}}
def end_if(state, ctx):
    for a,b,c in LINES:
        v = state["cells"][a]
        if v != None and v == state["cells"][b] and v == state["cells"][c]:
            return {"winner": "1" if v == "0" else "0"}
    if None not in state["cells"]: return {"draw": True}
    return None
def legal_moves(state, ctx):
    return [{"name":"click","args":[i]} for i in range(9) if state["cells"][i] == None]
`

// TestIntegration_FullDesignAndPlay exercises the complete "design a game and
// play it" pipeline through the Tools methods (not through match.Manager
// directly). It registers a Starlark game, creates a match, joins twice, plays
// five moves that complete the top row, and asserts that the misère winner is
// the player who did NOT complete the row.
func TestIntegration_FullDesignAndPlay(t *testing.T) {
	mgr := match.NewManager(storage.NewMemory())
	mgr.MustRegister(tictactoe.New())
	reg := NewUserAwareRegistry(mgr, NewInMemoryUserGames())
	tools := &Tools{Manager: mgr, Registry: reg}

	ctx := contextWithUser(context.Background(), "alice")

	// 1. Register the misère spec.
	rr, err := tools.RegisterGame(ctx, RegisterGameArgs{
		Source:   misereTTTIntegration,
		LLMGuide: "Don't make three in a row — the completer LOSES.",
	})
	if err != nil {
		t.Fatalf("RegisterGame: %v", err)
	}
	if rr.Name != "misere-ttt-i" {
		t.Errorf("RegisterGame.Name = %q, expected 'misere-ttt-i'", rr.Name)
	}

	// 2. list_games — alice sees both built-in tic-tac-toe and misere-ttt-i.
	list, err := tools.ListGames(ctx)
	if err != nil {
		t.Fatalf("ListGames: %v", err)
	}
	if len(list.Games) != 2 {
		t.Fatalf("alice's list_games returned %d game(s), expected 2: %v", len(list.Games), list.Games)
	}

	// 3. create_match for the user-designed game.
	cm, err := tools.CreateMatch(ctx, CreateMatchArgs{Game: "misere-ttt-i", NumPlayers: 2})
	if err != nil {
		t.Fatalf("CreateMatch: %v", err)
	}
	if cm.MatchID == "" {
		t.Fatal("CreateMatch returned empty MatchID")
	}

	// 4. join_match twice to assign PlayerID "0" and "1".
	jr0, err := tools.JoinMatch(ctx, JoinMatchArgs{MatchID: cm.MatchID})
	if err != nil {
		t.Fatalf("JoinMatch 0: %v", err)
	}
	jr1, err := tools.JoinMatch(ctx, JoinMatchArgs{MatchID: cm.MatchID})
	if err != nil {
		t.Fatalf("JoinMatch 1: %v", err)
	}
	if jr0.PlayerID == jr1.PlayerID {
		t.Fatalf("both joins returned the same PlayerID %q", jr0.PlayerID)
	}

	// 5. Play out: player 0 marks cells 0, 1, 2 (top row) → in misère, P0 loses.
	// Interleaved with player 1 marking cells 3 and 4 to keep turns valid.
	// Args are passed as int64 to match the Starlark int conversion path used
	// in direct (non-JSON) test calls.
	plays := []struct {
		pid, creds string
		idx        int64
	}{
		{jr0.PlayerID, jr0.Credentials, 0}, // P0: cell 0
		{jr1.PlayerID, jr1.Credentials, 3}, // P1: cell 3
		{jr0.PlayerID, jr0.Credentials, 1}, // P0: cell 1
		{jr1.PlayerID, jr1.Credentials, 4}, // P1: cell 4
		{jr0.PlayerID, jr0.Credentials, 2}, // P0: cell 2 → top row complete → P0 LOSES
	}
	for i, p := range plays {
		_, err := tools.MakeMove(ctx, MakeMoveArgs{
			MatchID:     cm.MatchID,
			PlayerID:    p.pid,
			Credentials: p.creds,
			Move:        "click",
			Args:        []any{p.idx},
		})
		if err != nil {
			t.Fatalf("move %d (P%s cell %d): %v", i, p.pid, p.idx, err)
		}
	}

	// 6. get_state — gameover.winner must be "1" (P0 completed the row, so P1 wins).
	gs, err := tools.GetState(ctx, GetStateArgs{MatchID: cm.MatchID})
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if gs.Gameover == nil {
		t.Fatal("GetState.Gameover is nil; game should be over")
	}
	go_, ok := gs.Gameover.(map[string]any)
	if !ok {
		t.Fatalf("GetState.Gameover is %T, expected map[string]any: %#v", gs.Gameover, gs.Gameover)
	}
	if go_["winner"] != "1" {
		t.Fatalf("expected winner='1' (P0 lost via misère), got gameover=%#v", go_)
	}

	// 7. GetState.Game must report the public game name (Task 18 fix).
	if gs.Game != "misere-ttt-i" {
		t.Errorf("GetState.Game = %q, expected public name 'misere-ttt-i'", gs.Game)
	}
}
