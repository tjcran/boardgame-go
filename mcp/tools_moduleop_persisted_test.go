package mcp

import (
	"context"
	"path/filepath"
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

// TestModuleOp_PersistedMatch_NowWorks replaces the old "ActionableError"
// test from PR #72. Now that AsStarlarkG rehydrates live module states from
// the bare map a serializing store returns, module_op works on persisted
// matches — no error, correct result.
func TestModuleOp_PersistedMatch_NowWorks(t *testing.T) {
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

	// setup placed unit 1 at (0,0); position_of must succeed and return [0,0].
	res, err := (&Tools{Manager: mgr}).ModuleOp(context.Background(), ModuleOpArgs{
		MatchID: id, Module: "tabletop", Op: "position_of", Args: map[string]any{"unit": int64(1)},
	})
	if err != nil {
		t.Fatalf("module_op on persisted match: %v", err)
	}
	list, ok := res.Result.([]any)
	if !ok {
		t.Fatalf("position_of result type = %T, want []any", res.Result)
	}
	if len(list) != 2 {
		t.Fatalf("position_of result len = %d, want 2; got %v", len(list), list)
	}
	if list[0] != int64(0) {
		t.Errorf("position_of x = %v (%T), want int64(0)", list[0], list[0])
	}
	if list[1] != int64(0) {
		t.Errorf("position_of y = %v (%T), want int64(0)", list[1], list[1])
	}
}
