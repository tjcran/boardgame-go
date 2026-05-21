package starlarkgame

import (
	"context"
	"strings"
	"testing"

	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/match"
	"github.com/tjcran/boardgame-go/storage"
)

// legal_moves reads board geometry from tabletop (a read op) — the
// Stratego-shaped case: you cannot enumerate legal moves without querying
// piece positions / neighbors.
const readSideLegalMovesSpec = `
META = {"name": "rs-legal", "min_players": 1, "max_players": 1}
MODULES = ["tabletop"]
def setup(ctx):
    ctx.modules.tabletop.new_board(kind="square", w=4, h=4)
    ctx.modules.tabletop.place(unit=1, x=1, y=1)
    return {}
def end_if(state, ctx): return None
def legal_moves(state, ctx):
    ns = ctx.modules.tabletop.neighbors(x=1, y=1)
    if len(ns) > 0:
        return [{"name": "noop", "args": []}]
    return []
def noop(state, ctx): return state
MOVES = {"noop": {"apply": noop}}
`

// end_if reads board state (a read op) to decide gameover.
const readSideEndIfSpec = `
META = {"name": "rs-endif", "min_players": 1, "max_players": 1}
MODULES = ["tabletop"]
def setup(ctx):
    ctx.modules.tabletop.new_board(kind="square", w=4, h=4)
    ctx.modules.tabletop.place(unit=1, x=2, y=2)
    return {}
def end_if(state, ctx):
    # (0,0) is empty at setup -> None; a "goto" move brings the unit here.
    if len(ctx.modules.tabletop.entities_at(x=0, y=0)) > 0:
        return {"winner": "0"}
    return None
def legal_moves(state, ctx): return [{"name": "goto", "args": []}]
def goto(state, ctx):
    ctx.modules.tabletop.move(unit=1, x=0, y=0)
    return state
MOVES = {"goto": {"apply": goto}}
`

// legal_moves attempts a MUTATING op (move). The read-only contract must
// reject it so speculative enumeration cannot corrupt the shared live
// board — and Validate surfaces that at registration time.
const readSideMutateRejectedSpec = `
META = {"name": "rs-mutate", "min_players": 1, "max_players": 1}
MODULES = ["tabletop"]
def setup(ctx):
    ctx.modules.tabletop.new_board(kind="square", w=4, h=4)
    ctx.modules.tabletop.place(unit=1, x=1, y=1)
    return {}
def end_if(state, ctx): return None
def legal_moves(state, ctx):
    ctx.modules.tabletop.move(unit=1, x=3, y=3)
    return [{"name": "noop", "args": []}]
def noop(state, ctx): return state
MOVES = {"noop": {"apply": noop}}
`

func buildAndJoin(t *testing.T, src, name string) (*core.Game, *match.Manager, string, string, string) {
	t.Helper()
	spec, err := LoadSpec(src)
	if err != nil {
		t.Fatalf("LoadSpec: %v", err)
	}
	if err := Validate(context.Background(), spec); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	g := BuildCoreGame(spec)
	mgr := match.NewManager(storage.NewMemory())
	mgr.MustRegister(g)
	id, err := mgr.Create(name, match.CreateOptions{NumPlayers: 1})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	jr, err := mgr.Join(id, "p", match.JoinOptions{})
	if err != nil {
		t.Fatalf("Join: %v", err)
	}
	return g, mgr, id, jr.PlayerID, jr.PlayerCredentials
}

func TestLegalMoves_CanReadModules(t *testing.T) {
	g, mgr, id, pid, _ := buildAndJoin(t, readSideLegalMovesSpec, "rs-legal")
	st, _ := mgr.State(id)
	acts := g.Enumerate(st.State.G, st.State.Ctx, pid)
	if len(acts) != 1 || acts[0].Move != "noop" {
		t.Fatalf("legal_moves could not read modules; acts=%v", acts)
	}
}

func TestEndIf_CanReadModules(t *testing.T) {
	_, mgr, id, pid, creds := buildAndJoin(t, readSideEndIfSpec, "rs-endif")
	if st, _ := mgr.State(id); st.State.Ctx.Gameover != nil {
		t.Fatalf("game over before any move")
	}
	if _, err := mgr.MoveReq(id, pid, creds, core.MoveRequest{Move: "goto"}); err != nil {
		t.Fatalf("goto move: %v", err)
	}
	st, _ := mgr.State(id)
	if st.State.Ctx.Gameover == nil {
		t.Fatalf("end_if did not read modules to set gameover after move")
	}
}

func TestLegalMoves_MutatingOpRejected(t *testing.T) {
	spec, err := LoadSpec(readSideMutateRejectedSpec)
	if err != nil {
		t.Fatalf("LoadSpec: %v", err)
	}
	err = Validate(context.Background(), spec)
	if err == nil {
		t.Fatalf("Validate accepted a spec calling a mutating op from legal_moves")
	}
	if !strings.Contains(err.Error(), "mutates state") {
		t.Fatalf("error did not explain the read-only violation: %v", err)
	}
}
