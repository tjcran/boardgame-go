package mcp

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tjcran/boardgame-go/match"
	"github.com/tjcran/boardgame-go/mcp/starlarkgame"
	sqlitestore "github.com/tjcran/boardgame-go/storage/sqlite"
)

const moduleOpPersistedSpec = `
META = {"name": "moe", "min_players": 1, "max_players": 1}
MODULES = ["tabletop"]
def setup(ctx):
    ctx.modules.tabletop.new_board(kind="square", w=3, h=3)
    ctx.modules.tabletop.place(unit=1, x=0, y=0)
    return {}
def end_if(state, ctx): return None
def legal_moves(state, ctx): return [{"name": "noop", "args": []}]
def noop(state, ctx): return state
MOVES = {"noop": {"apply": noop}}
`

// On a serializing store the match's module state isn't live, so module_op
// can't run. It must say so in an actionable way — not the misleading
// "not a designed game", which sent the design session down a rabbit hole.
func TestModuleOp_PersistedMatch_ActionableError(t *testing.T) {
	store, err := sqlitestore.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("sqlite open: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	spec, err := starlarkgame.LoadSpec(moduleOpPersistedSpec)
	if err != nil {
		t.Fatalf("LoadSpec: %v", err)
	}
	g := starlarkgame.BuildCoreGame(spec)
	mgr := match.NewManager(store)
	mgr.MustRegister(g)
	id, err := mgr.Create("moe", match.CreateOptions{NumPlayers: 1})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, err = (&Tools{Manager: mgr}).ModuleOp(context.Background(), ModuleOpArgs{
		MatchID: id, Module: "tabletop", Op: "position_of", Args: map[string]any{"unit": 1},
	})
	if err == nil {
		t.Fatalf("expected an error for module_op on a persisted match")
	}
	msg := err.Error()
	if strings.Contains(msg, "not a designed game") {
		t.Fatalf("misleading error retained: %q", msg)
	}
	if !strings.Contains(msg, "in-memory") {
		t.Fatalf("error not actionable (should point at the in-memory store): %q", msg)
	}
}
