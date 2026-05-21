package mcp

import (
	"context"
	"testing"

	"github.com/tjcran/boardgame-go/match"
	"github.com/tjcran/boardgame-go/mcp/starlarkgame"
	"github.com/tjcran/boardgame-go/storage"
)

// A spec whose setup leaves modules empty so the tool calls drive all state.
const miniCCGSpecForTools = `
META = {"name": "mini-ccg", "min_players": 1, "max_players": 1}
MODULES = ["ccg"]
def setup(ctx): return {}
def end_if(state, ctx): return None
def legal_moves(state, ctx): return []
MOVES = {"noop": {"apply": lambda state, ctx: state}}
`

func TestModuleTool_ParityWithStarlark(t *testing.T) {
	spec, err := starlarkgame.LoadSpec(miniCCGSpecForTools)
	if err != nil {
		t.Fatalf("LoadSpec: %v", err)
	}
	g := starlarkgame.BuildCoreGame(spec)
	mgr := match.NewManager(storage.NewMemory())
	mgr.MustRegister(g)
	id, _ := mgr.Create("mini-ccg", match.CreateOptions{NumPlayers: 1})
	mgr.Join(id, "p", match.JoinOptions{})

	tools := &Tools{Manager: mgr}

	if _, err := tools.ModuleOp(context.Background(), ModuleOpArgs{
		MatchID: id, Module: "ccg", Op: "new_zone",
		Args: map[string]any{"name": "deck", "ordered": true},
	}); err != nil {
		t.Fatalf("new_zone tool: %v", err)
	}
	// new_entity allocates the entity but does NOT place it in any zone
	// (Entity.Zone starts empty); an explicit move_to is required, exactly
	// as the canonical modulebridge/ccg parity test does. We thread the
	// returned entity token straight back through the tool to prove the
	// SAME live *ccg.State is mutated across consecutive ModuleOp calls.
	ent, err := tools.ModuleOp(context.Background(), ModuleOpArgs{
		MatchID: id, Module: "ccg", Op: "new_entity",
		Args: map[string]any{"type": "card", "owner": "0"},
	})
	if err != nil {
		t.Fatalf("new_entity tool: %v", err)
	}
	tok, ok := ent.Result.(string)
	if !ok {
		t.Fatalf("new_entity returned %T, want entity token string", ent.Result)
	}
	if _, err := tools.ModuleOp(context.Background(), ModuleOpArgs{
		MatchID: id, Module: "ccg", Op: "move_to",
		Args: map[string]any{"entity": tok, "zone": "deck"},
	}); err != nil {
		t.Fatalf("move_to tool: %v", err)
	}
	res, err := tools.ModuleOp(context.Background(), ModuleOpArgs{
		MatchID: id, Module: "ccg", Op: "size", Args: map[string]any{"zone": "deck"},
	})
	if err != nil {
		t.Fatalf("size tool: %v", err)
	}
	if res.Result.(int64) != 1 {
		t.Fatalf("got deck size %v via tool, want 1", res.Result)
	}
}
