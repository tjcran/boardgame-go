package starlarkgame_test

import (
	"context"
	"testing"

	"github.com/tjcran/boardgame-go/match"
	"github.com/tjcran/boardgame-go/mcp/starlarkgame"
	"github.com/tjcran/boardgame-go/storage"
)

// stagedSpec models a loveletter-Prince–shaped pattern: the current
// player can play `card`, which doesn't end their turn — instead it
// pushes them into a "targeting" stage where their only legal move
// is `pick_target`. Calling pick_target applies the effect, leaves
// the stage, and ends the turn.
const stagedSpec = `
META = {"name":"staged","min_players":2,"max_players":2}

def setup(ctx):
    return {"affected": None, "actor": None}

def _card(state, ctx):
    ctx.events.set_stage("targeting")
    return {"affected": state["affected"], "actor": ctx.player_id}

def _pick(state, ctx, target):
    ctx.events.end_stage()
    return {"affected": target, "actor": state["actor"]}

MOVES = {
    "card": {"args": [], "apply": _card, "ends_turn": False},
    "pick_target": {"args": [{"name":"target","type":"string"}], "apply": _pick},
}

STAGES = {
    "targeting": {
        "moves": {
            "pick_target": {"args": [{"name":"target","type":"string"}], "apply": _pick},
        },
    },
}

def end_if(state, ctx):
    if state["affected"] != None:
        return {"winner": state["actor"]}
    return None

def legal_moves(state, ctx):
    if ctx.phase == "" and state["affected"] != None:
        return []
    return [{"move":"card","args":[]}, {"move":"pick_target","args":["1"]}]
`

func TestStages_PrincePattern_SetThenEnd(t *testing.T) {
	spec, err := starlarkgame.LoadSpec(stagedSpec)
	if err != nil {
		t.Fatalf("LoadSpec: %v", err)
	}
	if err := starlarkgame.Validate(context.Background(), spec); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if _, ok := spec.Stages["targeting"]; !ok {
		t.Fatalf("Stages[\"targeting\"] missing: %v", spec.Stages)
	}

	mgr := match.NewManager(storage.NewMemory())
	mgr.MustRegister(starlarkgame.BuildCoreGame(spec))

	mid, _ := mgr.Create("staged", match.CreateOptions{NumPlayers: 2})
	j0, _ := mgr.Join(mid, "p0", match.JoinOptions{PlayerID: "0"})
	j1, _ := mgr.Join(mid, "p1", match.JoinOptions{PlayerID: "1"})

	// P0 plays `card` — ends_turn: False AND set_stage("targeting")
	// keeps P0 active and gated into targeting.
	if _, err := mgr.Move(mid, j0.PlayerID, j0.PlayerCredentials, "card", nil); err != nil {
		t.Fatalf("P0 card: %v", err)
	}
	m, _ := mgr.State(mid)
	if m.State.Ctx.CurrentPlayer != "0" {
		t.Errorf("after card, CurrentPlayer = %q, want 0 (P0 still active in targeting stage)", m.State.Ctx.CurrentPlayer)
	}
	if stage, ok := m.State.Ctx.ActivePlayers["0"]; !ok || stage != "targeting" {
		t.Errorf("P0 should be in targeting stage, ActivePlayers = %#v", m.State.Ctx.ActivePlayers)
	}

	// P1 cannot move while P0 is in the targeting stage.
	if _, err := mgr.Move(mid, j1.PlayerID, j1.PlayerCredentials, "pick_target", []any{"0"}); err == nil {
		t.Fatalf("expected wrong-player rejection for P1 during P0's targeting stage")
	}

	// P0 picks a target — pick_target ends the stage AND (default ends_turn=true) ends the turn.
	if _, err := mgr.Move(mid, j0.PlayerID, j0.PlayerCredentials, "pick_target", []any{"1"}); err != nil {
		t.Fatalf("P0 pick_target: %v", err)
	}
	m, _ = mgr.State(mid)
	g, ok := m.State.G.(map[string]any)
	if !ok {
		t.Fatalf("G is not map: %T", m.State.G)
	}
	if g["affected"] != "1" || g["actor"] != "0" {
		t.Errorf("after pick_target, G = %#v; want affected=1 actor=0", g)
	}
	if m.State.Ctx.Gameover == nil {
		t.Fatalf("expected gameover after pick_target (end_if returns winner)")
	}
}

func TestStages_RejectMissingMoves(t *testing.T) {
	_, err := starlarkgame.LoadSpec(`
META = {"name":"x","min_players":2,"max_players":2}
def setup(ctx): return {}
def _a(state, ctx): return state
MOVES = {"a": {"args": [], "apply": _a}}
STAGES = {"bad": {}}  # no moves
def end_if(state, ctx): return None
def legal_moves(state, ctx): return []
`)
	if err == nil {
		t.Fatalf("expected error on STAGES entry missing moves")
	}
}

func TestStages_PhasesOnly_StillBuilds(t *testing.T) {
	// A spec that uses PHASES but no STAGES still builds; Game.Turn is nil.
	spec, _ := starlarkgame.LoadSpec(`
META = {"name":"p","min_players":2,"max_players":2}
def setup(ctx): return {}
def _a(state, ctx): return state
PHASES = {"only": {"moves": {"a": {"args":[], "apply": _a}}, "start": True}}
MOVES = {"a": {"args":[], "apply": _a}}
def end_if(state, ctx): return None
def legal_moves(state, ctx): return []
`)
	g := starlarkgame.BuildCoreGame(spec)
	if g.Turn != nil {
		t.Errorf("Game.Turn should be nil when STAGES is empty, got %+v", g.Turn)
	}
	if g.Phases == nil {
		t.Errorf("Game.Phases should still be populated")
	}
}
