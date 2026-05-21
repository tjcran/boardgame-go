package starlarkgame

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/match"
	"github.com/tjcran/boardgame-go/storage"
)

const boltSpec = `
META = {"name": "bolt", "min_players": 2, "max_players": 2}

def setup(ctx):
    return {"creatures": ["ent:1", "ent:2"], "dead": []}

def end_if(state, ctx): return None
def legal_moves(state, ctx): return []

def cast_bolt(state, ctx):
    ctx.request_target(kind="creature", candidates=state["creatures"], min=1, max=1)
    return state

def resolve_bolt(state, ctx, target):
    ctx.validate_selection([target])
    dead = list(state["dead"])
    dead.append(target)
    remaining = [c for c in state["creatures"] if c != target]
    return {"creatures": remaining, "dead": dead}

MOVES = {
    "cast_bolt": {"apply": cast_bolt, "ends_turn": False},
    "resolve_bolt": {"apply": resolve_bolt, "ends_turn": False, "args": [{"name": "target", "type": "string"}]},
}
`

func TestBolt_RequestResolveReplay(t *testing.T) {
	spec, err := LoadSpec(boltSpec)
	if err != nil {
		t.Fatalf("LoadSpec: %v", err)
	}
	if err := Validate(context.Background(), spec); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	g := BuildCoreGame(spec)

	mgr := match.NewManager(storage.NewMemory())
	mgr.MustRegister(g)
	id, err := mgr.Create("bolt", match.CreateOptions{NumPlayers: 2})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	j0, _ := mgr.Join(id, "p0", match.JoinOptions{})
	mgr.Join(id, "p1", match.JoinOptions{})

	if _, err := mgr.MoveReq(id, j0.PlayerID, j0.PlayerCredentials, core.MoveRequest{Move: "cast_bolt"}); err != nil {
		t.Fatalf("cast_bolt: %v", err)
	}
	m, _ := mgr.State(id)
	if len(m.State.Blocks) != 1 || m.State.Blocks[0].Tag != "creature" {
		t.Fatalf("expected one pending 'creature' block, got %#v", m.State.Blocks)
	}

	if _, err := mgr.MoveReq(id, j0.PlayerID, j0.PlayerCredentials, core.MoveRequest{Move: "cast_bolt"}); !errors.Is(err, core.ErrBlocked) {
		t.Fatalf("expected ErrBlocked, got %v", err)
	}

	if _, err := mgr.MoveReq(id, j0.PlayerID, j0.PlayerCredentials, core.MoveRequest{
		Move:      "resolve_bolt",
		ResumeTag: "creature",
		Args:      []any{"ent:1"},
	}); err != nil {
		t.Fatalf("resolve_bolt: %v", err)
	}

	m2, _ := mgr.State(id)
	if len(m2.State.Blocks) != 0 {
		t.Fatalf("block should be cleared after resume, got %#v", m2.State.Blocks)
	}
	sg := m2.State.G.(*StarlarkG)
	dead, _ := sg.Data["dead"].([]any)
	if len(dead) != 1 || dead[0] != "ent:1" {
		t.Fatalf("dead = %#v, want [ent:1]", sg.Data["dead"])
	}

	replayed, err := core.Replay(g, m2.State.Log, 2, nil)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	orig, _ := json.Marshal(m2.State.G)
	repl, _ := json.Marshal(replayed.G)
	if string(orig) != string(repl) {
		t.Fatalf("replay mismatch:\n orig=%s\n repl=%s", orig, repl)
	}
}
