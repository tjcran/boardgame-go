package starlarkgame

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/match"
	"github.com/tjcran/boardgame-go/storage"
	sqlitestore "github.com/tjcran/boardgame-go/storage/sqlite"
)

// hooksGameSpec is defined in modules_hooks_test.go (same package).
// We reuse it here for the SQLite-store variant.

// TestHooks_SQLite_HookFiresAfterReload verifies that a designed ccg game's
// HOOKS handler still fires after the match state has been persisted and
// reloaded by a serializing store (SQLite). The in-memory store test
// (TestHooksGame_SlayDrawsAndReplays) passes before this fix because the
// live *StarlarkG is carried across moves without a JSON round-trip;
// this test would FAIL before the move-closure change that re-registers
// hooks on a fresh-rehydrated ccg.State.
func TestHooks_SQLite_HookFiresAfterReload(t *testing.T) {
	store, err := sqlitestore.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("sqlite open: %v", err)
	}
	defer store.Close()

	spec, err := LoadSpec(hooksGameSpec)
	if err != nil {
		t.Fatalf("LoadSpec: %v", err)
	}
	if err := Validate(context.Background(), spec); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	g := BuildCoreGame(spec)
	mgr := match.NewManager(store)
	mgr.MustRegister(g)

	id, err := mgr.Create("hooksgame", match.CreateOptions{NumPlayers: 1})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	jr, err := mgr.Join(id, "p", match.JoinOptions{})
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	// "slay" moves the creature to grave and publishes "died".
	// The HOOKS handler on "died" draws a card from deck to hand.
	// After one slay: hand=1, deck=1, grave=1.
	// Without the fix the hook doesn't fire after the SQLite reload
	// (fresh ccg.State has no subscriptions) so hand=0, deck=2, grave=1.
	if _, err := mgr.MoveReq(id, jr.PlayerID, jr.PlayerCredentials, core.MoveRequest{Move: "slay"}); err != nil {
		t.Fatalf("slay: %v", err)
	}

	m, err := mgr.State(id)
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	sg, ok := asStarlarkG(m.State.G)
	if !ok {
		t.Fatalf("state is not StarlarkG-compatible: %T", m.State.G)
	}
	cs := ccgStateOf(t, sg)
	if cs.Size("hand") != 1 || cs.Size("deck") != 1 || cs.Size("grave") != 1 {
		t.Fatalf("after slay (SQLite): hand=%d deck=%d grave=%d, want 1/1/1 — hook did not fire after reload",
			cs.Size("hand"), cs.Size("deck"), cs.Size("grave"))
	}
}

// TestHooks_InMemory_NoDoubleFire verifies that on the in-memory store the
// HOOKS handler fires EXACTLY ONCE per triggering move, not twice. This is the
// regression guard for the wasLive check: if someone removes the guard,
// re-registering on the already-subscribed *StarlarkG would double-fire.
//
// We use a counting spec: each hook invocation appends 1 to a list in state,
// so "fired twice" is directly observable.
const hooksCountSpec = `
META = {"name": "hookscount", "min_players": 1, "max_players": 1}
MODULES = ["ccg"]

def setup(ctx):
    ctx.modules.ccg.new_zone(name="deck", ordered=True)
    ctx.modules.ccg.new_zone(name="grave", ordered=False)
    c = ctx.modules.ccg.new_entity(type="card", owner="0")
    ctx.modules.ccg.move_to(entity=c, zone="deck")
    return {"fires": 0}

def end_if(state, ctx): return None
def legal_moves(state, ctx): return [{"name": "slay", "args": []}]

def on_died(event, ctx):
    # No ctx.state write — use ccg as a side-channel counter:
    # move a card from deck to grave each time handler fires.
    top = ctx.modules.ccg.draw(zone="deck", n=1)
    ctx.modules.ccg.move_to(entity=top[0], zone="grave")

HOOKS = {"died": on_died}

def slay(state, ctx):
    # Publish "died"; the hook should fire exactly once.
    ctx.modules.ccg.publish(type="died")
    return state

MOVES = {"slay": {"apply": slay}}
`

func TestHooks_InMemory_NoDoubleFire(t *testing.T) {
	spec, err := LoadSpec(hooksCountSpec)
	if err != nil {
		t.Fatalf("LoadSpec: %v", err)
	}
	if err := Validate(context.Background(), spec); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	g := BuildCoreGame(spec)
	mgr := match.NewManager(storage.NewMemory())
	mgr.MustRegister(g)

	id, err := mgr.Create("hookscount", match.CreateOptions{NumPlayers: 1})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	jr, err := mgr.Join(id, "p", match.JoinOptions{})
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	if _, err := mgr.MoveReq(id, jr.PlayerID, jr.PlayerCredentials, core.MoveRequest{Move: "slay"}); err != nil {
		t.Fatalf("slay: %v", err)
	}

	m, err := mgr.State(id)
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	sg, ok := m.State.G.(*StarlarkG)
	if !ok {
		t.Fatalf("state is not *StarlarkG: %T", m.State.G)
	}
	cs := ccgStateOf(t, sg)

	// The hook moves one card from deck to grave each time it fires.
	// deck started with 1 card. After one slay:
	//  - fired once  → deck=0, grave=1  ✓
	//  - fired twice → deck=-1? (draw panics) or grave=2 if deck had >1
	// We set up exactly 1 card, so a second firing would panic on draw.
	// Check grave=1 (and deck=0) to confirm exactly-once.
	if cs.Size("grave") != 1 {
		t.Fatalf("hook fired %d time(s): grave=%d, want 1 — double-fire bug",
			cs.Size("grave"), cs.Size("grave"))
	}
	if cs.Size("deck") != 0 {
		t.Fatalf("unexpected deck size after slay: deck=%d, want 0", cs.Size("deck"))
	}
}
