package mcp

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/tjcran/boardgame-go/match"
	"github.com/tjcran/boardgame-go/mcp/starlarkgame"
	sqlitestore "github.com/tjcran/boardgame-go/storage/sqlite"
)

// sqttSpec is a tabletop-backed single-player game whose "go" move reads
// back the moved position via the live module. Used to prove both that
// moves work end-to-end on a SQLite-backed manager
// (TestModuleGame_SQLite_MovePlayable) and that module_op can rehydrate the
// live state instead of erroring (TestModuleOp_SQLite_ReadsRehydratedState).
const sqttSpec = `
META = {"name": "sqtt", "min_players": 1, "max_players": 1}
MODULES = ["tabletop"]
def setup(ctx):
    ctx.modules.tabletop.new_board(kind="square", w=3, h=3)
    ctx.modules.tabletop.place(unit=1, x=0, y=0)
    return {}
def end_if(state, ctx): return None
def legal_moves(state, ctx): return [{"name": "go", "args": []}]
def go(state, ctx):
    ctx.modules.tabletop.move(unit=1, x=1, y=1)
    p = ctx.modules.tabletop.position_of(unit=1)
    return {"px": p[0], "py": p[1]}
MOVES = {"go": {"apply": go}}
`

// toFloat normalises numeric any values (int64, float64) to float64 so test
// assertions don't break on the JSON-decode int64→float64 difference.
func toFloat(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int64:
		return float64(n)
	case int:
		return float64(n)
	}
	return 0
}

// sqttFixture creates a SQLite-backed manager with the sqtt game registered,
// creates a match, and joins the single player. Returns the manager, match
// ID, and the join result.
func sqttFixture(t *testing.T) (*match.Manager, string, match.JoinResult) {
	t.Helper()
	store, err := sqlitestore.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("sqlite open: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	spec, err := starlarkgame.LoadSpec(sqttSpec)
	if err != nil {
		t.Fatalf("LoadSpec: %v", err)
	}
	if err := starlarkgame.Validate(context.Background(), spec); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	g := starlarkgame.BuildCoreGame(spec)
	mgr := match.NewManager(store)
	mgr.MustRegister(g)

	id, err := mgr.Create("sqtt", match.CreateOptions{NumPlayers: 1})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	j, err := mgr.Join(id, "p0", match.JoinOptions{PlayerID: "0"})
	if err != nil {
		t.Fatalf("Join: %v", err)
	}
	return mgr, id, j
}

// TestModuleGame_SQLite_MovePlayable is the headline fix: a module move that
// reads+writes tabletop state must succeed after the match round-trips
// through SQLite (rehydration must kick in on the move path).
func TestModuleGame_SQLite_MovePlayable(t *testing.T) {
	mgr, id, j := sqttFixture(t)

	_, err := mgr.Move(id, j.PlayerID, j.PlayerCredentials, "go", nil)
	if err != nil {
		t.Fatalf("Move (go) on SQLite store: %v", err)
	}

	m, err := mgr.State(id)
	if err != nil {
		t.Fatalf("State after move: %v", err)
	}

	// The "go" move calls position_of after moving and stores px/py in Data.
	// After JSON round-trip through the store G is a map[string]any; numeric
	// values decode as float64 (standard JSON decode to any). AsStarlarkG
	// normalises the G to a *StarlarkG while rehydrating module states.
	sg, ok := starlarkgame.AsStarlarkG(m.State.G)
	if !ok {
		t.Fatalf("state is not a StarlarkG (got %T)", m.State.G)
	}
	px, pxOK := sg.Data["px"]
	py, pyOK := sg.Data["py"]
	if !pxOK || !pyOK {
		t.Fatalf("Data missing px/py after move; Data = %v", sg.Data)
	}
	// JSON unmarshals numbers to float64 when the target is `any`.
	if toFloat(px) != 1.0 {
		t.Errorf("px = %v (%T), want 1", px, px)
	}
	if toFloat(py) != 1.0 {
		t.Errorf("py = %v (%T), want 1", py, py)
	}
}

// TestModuleOp_SQLite_ReadsRehydratedState proves that module_op no longer
// errors with the old #72 "in-memory store" message when the match was loaded
// from a serializing store. It must rehydrate and return the correct position
// of unit 1 after the "go" move.
func TestModuleOp_SQLite_ReadsRehydratedState(t *testing.T) {
	mgr, id, j := sqttFixture(t)

	// Apply the "go" move so unit 1 is at (1,1).
	if _, err := mgr.Move(id, j.PlayerID, j.PlayerCredentials, "go", nil); err != nil {
		t.Fatalf("Move (go): %v", err)
	}

	res, err := (&Tools{Manager: mgr}).ModuleOp(context.Background(), ModuleOpArgs{
		MatchID: id,
		Module:  "tabletop",
		Op:      "position_of",
		Args:    map[string]any{"unit": int64(1)},
	})
	if err != nil {
		t.Fatalf("ModuleOp position_of on SQLite store: %v", err)
	}

	// position_of returns []any{int64(x), int64(y)}.
	list, ok := res.Result.([]any)
	if !ok {
		t.Fatalf("position_of result type = %T, want []any; result = %v", res.Result, res.Result)
	}
	if len(list) != 2 {
		t.Fatalf("position_of result len = %d, want 2; got %v", len(list), list)
	}
	if list[0] != int64(1) {
		t.Errorf("position_of x = %v (%T), want int64(1)", list[0], list[0])
	}
	if list[1] != int64(1) {
		t.Errorf("position_of y = %v (%T), want int64(1)", list[1], list[1])
	}
}
