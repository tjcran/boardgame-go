package starlarkgame

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/match"
	"github.com/tjcran/boardgame-go/modules/ccg"
	"github.com/tjcran/boardgame-go/storage"
)

// Exercises the parity ops end-to-end through Starlark: modifiers with
// lifetime links (list kwarg), clone (dict kwarg), counters, query,
// and visibility — verifying kwarg conversion for lists and dicts,
// plus replay determinism over the new ops.
const parityCCGSpec = `
META = {"name": "parity-ccg", "min_players": 1, "max_players": 1}
MODULES = ["ccg"]

def setup(ctx):
    m = ctx.modules.ccg
    m.new_zone(name="battlefield", ordered=True)
    m.new_zone(name="graveyard", ordered=True)
    aura = m.new_entity(type="aura", owner="0")
    bear = m.new_entity(type="creature", owner="0", attrs={"power": 2})
    m.move_to(entity=aura, zone="battlefield")
    m.move_to(entity=bear, zone="battlefield")
    m.add_modifier(target=bear, attribute="power", op="add", value=2,
                   source=aura, expires_with=aura, while_in=["battlefield"])
    m.add_counter(entity=bear, kind="+1/+1", n=1)
    m.set_visibility(entity=bear, visibility="public")
    return {"aura": aura, "bear": bear}

def end_if(state, ctx): return None
def legal_moves(state, ctx): return [{"name": "bury_aura", "args": []}, {"name": "copy_bear", "args": []}]

def bury_aura(state, ctx):
    ctx.modules.ccg.move_to(entity=state["aura"], zone="graveyard")
    return state

def copy_bear(state, ctx):
    m = ctx.modules.ccg
    twin = m.clone(entity=state["bear"], owner="0", overrides={"token": True})
    m.move_to(entity=twin, zone="battlefield")
    return dict(state, twin=twin)

MOVES = {"bury_aura": {"apply": bury_aura}, "copy_bear": {"apply": copy_bear}}
`

// attrAsInt normalizes the numeric types EffectiveAttr can return —
// int from Go modifier math, int64 from Starlark-supplied base attrs.
func attrAsInt(t *testing.T, v any) int {
	t.Helper()
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	t.Fatalf("attr value %v (%T) is not numeric", v, v)
	return 0
}

func TestParityOps_ThroughStarlarkAndReplay(t *testing.T) {
	spec, err := LoadSpec(parityCCGSpec)
	if err != nil {
		t.Fatalf("LoadSpec: %v", err)
	}
	if err := Validate(context.Background(), spec); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	g := BuildCoreGame(spec)

	mgr := match.NewManager(storage.NewMemory())
	mgr.MustRegister(g)
	id, err := mgr.Create("parity-ccg", match.CreateOptions{NumPlayers: 1})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	jr, err := mgr.Join(id, "p", match.JoinOptions{})
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	st, _ := mgr.State(id)
	ccgState := ccgStateOf(t, st.State.G.(*StarlarkG))
	bear := ccg.Query(ccgState).HasType("creature").First()
	if bear == 0 {
		t.Fatal("bear not found after setup")
	}
	if got := attrAsInt(t, ccgState.EffectiveAttr(bear, "power", 0)); got != 4 {
		t.Fatalf("setup power = %v, want 4 (base 2 + aura 2)", got)
	}
	if ccgState.Counters(bear, "+1/+1") != 1 {
		t.Fatal("counter not applied through bridge")
	}

	if _, err := mgr.MoveReq(id, jr.PlayerID, jr.PlayerCredentials, core.MoveRequest{Move: "copy_bear"}); err != nil {
		t.Fatalf("copy_bear: %v", err)
	}
	if _, err := mgr.MoveReq(id, jr.PlayerID, jr.PlayerCredentials, core.MoveRequest{Move: "bury_aura"}); err != nil {
		t.Fatalf("bury_aura: %v", err)
	}

	st, _ = mgr.State(id)
	ccgState = ccgStateOf(t, st.State.G.(*StarlarkG))
	if n := ccg.Query(ccgState).HasType("creature").Count(); n != 2 {
		t.Fatalf("creatures after clone = %d, want 2", n)
	}
	if got := attrAsInt(t, ccgState.EffectiveAttr(bear, "power", 0)); got != 2 {
		t.Fatalf("power after aura buried = %v, want base 2 (lifetime-linked modifier expired)", got)
	}

	// Replay the recorded log and assert byte-identical final G — the
	// new ops must be as deterministic as the rest of the bridge.
	replayed, err := core.Replay(g, st.State.Log, 1, nil)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	origJSON, _ := json.Marshal(st.State.G)
	replayJSON, _ := json.Marshal(replayed.G)
	if string(origJSON) != string(replayJSON) {
		t.Fatalf("replay mismatch:\n orig=%s\n repl=%s", origJSON, replayJSON)
	}
}
