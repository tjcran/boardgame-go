package starlarkgame_test

import (
	"context"
	"testing"

	"github.com/tjcran/boardgame-go/match"
	"github.com/tjcran/boardgame-go/mcp/starlarkgame"
	"github.com/tjcran/boardgame-go/storage"
)

// multiActionSpec models a two-action turn: a player must call `roll`
// (ends_turn: False) then `commit` (ends_turn: True) before control
// passes to the next player. Trying to `commit` without rolling first
// is rejected by fail(); rolling twice is also rejected. This is the
// minimum shape that exercises the new per-move ends_turn flag end to
// end through match.Manager.
const multiActionSpec = `
META = {"name":"two-action","min_players":2,"max_players":2}

def setup(ctx):
    return {"rolled": {"0": False, "1": False}, "scores": {"0": 0, "1": 0}}

def _roll(state, ctx):
    pid = ctx.player_id
    if state["rolled"][pid]:
        fail("already rolled this turn")
    new_rolled = dict(state["rolled"])
    new_rolled[pid] = True
    new_scores = dict(state["scores"])
    new_scores[pid] = state["scores"][pid] + 1
    return {"rolled": new_rolled, "scores": new_scores}

def _commit(state, ctx):
    pid = ctx.player_id
    if not state["rolled"][pid]:
        fail("must roll before commit")
    new_rolled = dict(state["rolled"])
    new_rolled[pid] = False
    return {"rolled": new_rolled, "scores": state["scores"]}

MOVES = {
    "roll":   {"args": [], "apply": _roll,   "ends_turn": False},
    "commit": {"args": [], "apply": _commit, "ends_turn": True},
}

def end_if(state, ctx):
    for pid in ["0", "1"]:
        if state["scores"][pid] >= 3:
            return {"winner": pid}
    return None

def legal_moves(state, ctx):
    pid = ctx.player_id
    if state["rolled"][pid]:
        return [{"move": "commit", "args": []}]
    return [{"move": "roll", "args": []}]
`

func TestMultiActionTurns_NonTerminalMoveKeepsCurrentPlayer(t *testing.T) {
	spec, err := starlarkgame.LoadSpec(multiActionSpec)
	if err != nil {
		t.Fatalf("LoadSpec: %v", err)
	}
	if err := starlarkgame.Validate(context.Background(), spec); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	mgr := match.NewManager(storage.NewMemory())
	mgr.MustRegister(starlarkgame.BuildCoreGame(spec))

	mid, err := mgr.Create("two-action", match.CreateOptions{NumPlayers: 2})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	j0, _ := mgr.Join(mid, "p0", match.JoinOptions{PlayerID: "0"})
	j1, _ := mgr.Join(mid, "p1", match.JoinOptions{PlayerID: "1"})

	// Player 0 rolls — ends_turn: False, so it's still P0's turn.
	if _, err := mgr.Move(mid, j0.PlayerID, j0.PlayerCredentials, "roll", nil); err != nil {
		t.Fatalf("roll: %v", err)
	}
	m, _ := mgr.State(mid)
	if m.State.Ctx.CurrentPlayer != "0" {
		t.Fatalf("after non-terminal roll, CurrentPlayer = %q, want still 0", m.State.Ctx.CurrentPlayer)
	}

	// P1 trying to commit during P0's turn must be rejected by the
	// engine's player-gate (not by fail()) — the spec never sees it.
	if _, err := mgr.Move(mid, j1.PlayerID, j1.PlayerCredentials, "commit", nil); err == nil {
		t.Fatalf("expected wrong-player rejection for P1 during P0's turn")
	}

	// Player 0 commits — ends_turn: True, hands control to P1.
	if _, err := mgr.Move(mid, j0.PlayerID, j0.PlayerCredentials, "commit", nil); err != nil {
		t.Fatalf("commit: %v", err)
	}
	m, _ = mgr.State(mid)
	if m.State.Ctx.CurrentPlayer != "1" {
		t.Fatalf("after terminal commit, CurrentPlayer = %q, want 1", m.State.Ctx.CurrentPlayer)
	}
}

func TestMultiActionTurns_DefaultBehaviorUnchanged(t *testing.T) {
	// A spec that omits ends_turn entirely should keep the v0.4-v0.5.3
	// behaviour: every successful move ends the turn. This is the
	// back-compat guard for every existing designed game.
	const singleActionSpec = `
META = {"name":"single-action","min_players":2,"max_players":2}
def setup(ctx): return {"count": 0}
def _bump(state, ctx): return {"count": state["count"] + 1}
MOVES = {"bump": {"args": [], "apply": _bump}}
def end_if(state, ctx): return None
def legal_moves(state, ctx): return [{"move": "bump", "args": []}]
`
	spec, _ := starlarkgame.LoadSpec(singleActionSpec)
	mgr := match.NewManager(storage.NewMemory())
	mgr.MustRegister(starlarkgame.BuildCoreGame(spec))

	mid, _ := mgr.Create("single-action", match.CreateOptions{NumPlayers: 2})
	j0, _ := mgr.Join(mid, "p0", match.JoinOptions{PlayerID: "0"})
	j1, _ := mgr.Join(mid, "p1", match.JoinOptions{PlayerID: "1"})

	// Default ends_turn: True — alternation after each bump.
	if _, err := mgr.Move(mid, j0.PlayerID, j0.PlayerCredentials, "bump", nil); err != nil {
		t.Fatalf("P0 bump: %v", err)
	}
	m, _ := mgr.State(mid)
	if m.State.Ctx.CurrentPlayer != "1" {
		t.Fatalf("CurrentPlayer = %q after P0 bump, want 1", m.State.Ctx.CurrentPlayer)
	}
	if _, err := mgr.Move(mid, j1.PlayerID, j1.PlayerCredentials, "bump", nil); err != nil {
		t.Fatalf("P1 bump: %v", err)
	}
	m, _ = mgr.State(mid)
	if m.State.Ctx.CurrentPlayer != "0" {
		t.Fatalf("CurrentPlayer = %q after P1 bump, want 0", m.State.Ctx.CurrentPlayer)
	}
}
