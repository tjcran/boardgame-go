package mcp

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/tjcran/boardgame-go/match"
	"github.com/tjcran/boardgame-go/mcp/starlarkgame"
	sqlitestore "github.com/tjcran/boardgame-go/storage/sqlite"
)

// TestStarlarkGameMoves_WithSQLitePersistence reproduces the two bugs that
// shipped in v0.4.0 and bit the first end-to-end design+play attempt:
//
//  1. Ctx.ActivePlayers roundtrip: nil → {} on the wire → empty (non-nil)
//     map after unmarshal → authorisation reject every move with
//     ErrInactivePlayer. The Ctx.UnmarshalJSON fix in core/ctx.go restores
//     nil after the trip.
//
//  2. legal_moves dict key mismatch: the prompt told authors to use
//     "name" but the MCP response shape uses "move"; many authors wrote
//     {"move":...} and the adapter silently saw empty move names. The
//     legalMoveName helper now accepts both.
//
// Both bugs only manifest when state crosses the JSON boundary, which the
// in-memory storage tests miss — hence the SQLite-backed scenario here.
func TestStarlarkGameMoves_WithSQLitePersistence(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	store, err := sqlitestore.Open(dbPath)
	if err != nil {
		t.Fatalf("sqlite open: %v", err)
	}
	defer store.Close()

	const src = `
META = {"name":"simple","min_players":2,"max_players":2}
def setup(ctx): return {"turn": 0}
def _go(state, ctx):
    return {"turn": state["turn"] + 1}
MOVES = {"go": {"args":[], "apply": _go}}
def end_if(state, ctx):
    if state["turn"] >= 4: return {"winner": "0"}
    return None
def legal_moves(state, ctx):
    return [{"move":"go","args":[]}]
`
	spec, err := starlarkgame.LoadSpec(src)
	if err != nil {
		t.Fatalf("LoadSpec: %v", err)
	}
	if err := starlarkgame.Validate(context.Background(), spec); err != nil {
		t.Fatalf("Validate (accepts move-keyed legal_moves): %v", err)
	}

	mgr := match.NewManager(store)
	mgr.MustRegister(starlarkgame.BuildCoreGame(spec))

	mid, err := mgr.Create("simple", match.CreateOptions{NumPlayers: 2})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	j0, err := mgr.Join(mid, "p0", match.JoinOptions{PlayerID: "0"})
	if err != nil {
		t.Fatalf("Join 0: %v", err)
	}
	j1, err := mgr.Join(mid, "p1", match.JoinOptions{PlayerID: "1"})
	if err != nil {
		t.Fatalf("Join 1: %v", err)
	}

	// Four moves alternating players — would have failed every move on
	// v0.4.0 with "player is not active" because state roundtripped through
	// SQLite picked up an empty (non-nil) ActivePlayers map.
	for i, p := range []struct{ pid, creds string }{
		{j0.PlayerID, j0.PlayerCredentials},
		{j1.PlayerID, j1.PlayerCredentials},
		{j0.PlayerID, j0.PlayerCredentials},
		{j1.PlayerID, j1.PlayerCredentials},
	} {
		if _, err := mgr.Move(mid, p.pid, p.creds, "go", nil); err != nil {
			t.Fatalf("move %d (%s): %v", i, p.pid, err)
		}
	}

	final, err := mgr.State(mid)
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	if final.State.Ctx.Gameover == nil {
		t.Fatalf("expected gameover after 4 moves; got %#v", final.State.Ctx)
	}
}
