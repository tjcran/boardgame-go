package mcp

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/tjcran/boardgame-go/match"
	"github.com/tjcran/boardgame-go/mcp/starlarkgame"
	sqlitestore "github.com/tjcran/boardgame-go/storage/sqlite"
)

// sqccgSpec is a minimal one-player ccg game whose only move mints a new
// entity each call and appends the entity token to state["ids"]. After two
// moves the IDs list should contain two distinct tokens — proving that the
// ccg ID counter survived the JSON round-trip through SQLite between moves.
const sqccgSpec = `
META = {"name": "sqccg", "min_players": 1, "max_players": 1}
MODULES = ["ccg"]

def setup(ctx):
    ctx.modules.ccg.new_zone(name="deck", ordered=True)
    return {"ids": []}

def end_if(state, ctx): return None
def legal_moves(state, ctx): return [{"name": "mint", "args": []}]

def mint(state, ctx):
    e = ctx.modules.ccg.new_entity(type="card", owner="0")
    ctx.modules.ccg.move_to(entity=e, zone="deck")
    return {"ids": state["ids"] + [e]}

MOVES = {"mint": {"apply": mint}}
`

// TestCCGGame_SQLite_CounterPersistsAcrossMoves exercises the full
// match.Manager + SQLite path for a ccg-backed designed game.  Each "mint"
// move allocates a new entity; the two tokens must differ, which only holds
// when the ccg __counters__ JSON blob is reloaded correctly between moves.
func TestCCGGame_SQLite_CounterPersistsAcrossMoves(t *testing.T) {
	store, err := sqlitestore.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("sqlite open: %v", err)
	}
	defer store.Close()

	spec, err := starlarkgame.LoadSpec(sqccgSpec)
	if err != nil {
		t.Fatalf("LoadSpec: %v", err)
	}
	if err := starlarkgame.Validate(context.Background(), spec); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	mgr := match.NewManager(store)
	mgr.MustRegister(starlarkgame.BuildCoreGame(spec))

	mid, err := mgr.Create("sqccg", match.CreateOptions{NumPlayers: 1})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	j0, err := mgr.Join(mid, "p0", match.JoinOptions{PlayerID: "0"})
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	// Move 1: mint — allocates entity, persists to SQLite.
	if _, err := mgr.Move(mid, j0.PlayerID, j0.PlayerCredentials, "mint", nil); err != nil {
		t.Fatalf("move 1 (mint): %v", err)
	}

	// Move 2: mint again — loads state from SQLite, rehydrates ccg module
	// (including __counters__), allocates a second entity.
	if _, err := mgr.Move(mid, j0.PlayerID, j0.PlayerCredentials, "mint", nil); err != nil {
		t.Fatalf("move 2 (mint): %v", err)
	}

	final, err := mgr.State(mid)
	if err != nil {
		t.Fatalf("State: %v", err)
	}

	// Extract data["ids"] from the persisted game state.  After two JSON
	// round-trips the G comes back as a map[string]any; asStarlarkG unwraps it.
	sg, ok := starlarkgame.AsStarlarkG(final.State.G)
	if !ok {
		t.Fatalf("AsStarlarkG failed; G type = %T", final.State.G)
	}

	raw, ok := sg.Data["ids"]
	if !ok {
		t.Fatalf("Data[\"ids\"] missing; Data = %v", sg.Data)
	}
	ids, ok := raw.([]any)
	if !ok {
		t.Fatalf("Data[\"ids\"] has type %T, want []any", raw)
	}
	if len(ids) != 2 {
		t.Fatalf("expected 2 IDs, got %d: %v", len(ids), ids)
	}

	id1 := ids[0]
	id2 := ids[1]
	t.Logf("entity tokens after 2 mints: id1=%v  id2=%v", id1, id2)

	// Inequality of string tokens proves the counter was not reset to zero
	// between moves (counter persistence working correctly).
	if id1 == id2 {
		t.Fatalf("counter-persistence regression: both mints returned the same ID %v", id1)
	}
}
