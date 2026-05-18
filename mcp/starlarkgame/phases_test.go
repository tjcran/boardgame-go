package starlarkgame_test

import (
	"context"
	"testing"

	"github.com/tjcran/boardgame-go/match"
	"github.com/tjcran/boardgame-go/mcp/starlarkgame"
	"github.com/tjcran/boardgame-go/storage"
)

// phasedSpec models a setup-then-play game: each player must `ready`
// during the setup phase; when both have, the spec transitions to the
// play phase where they can call `act` to win.
const phasedSpec = `
META = {"name":"phased","min_players":2,"max_players":2}

def setup(ctx):
    return {"ready": {"0": False, "1": False}, "winner": None}

def _ready(state, ctx):
    new_ready = dict(state["ready"])
    new_ready[ctx.player_id] = True
    return {"ready": new_ready, "winner": state["winner"]}

def _setup_end_if(state, ctx):
    if state["ready"]["0"] and state["ready"]["1"]:
        return "play"
    return None

def _act(state, ctx):
    return {"ready": state["ready"], "winner": ctx.player_id}

PHASES = {
    "setup": {
        "moves": {"ready": {"args": [], "apply": _ready}},
        "end_if": _setup_end_if,
        "start": True,
    },
    "play": {
        "moves": {"act": {"args": [], "apply": _act}},
    },
}

# Top-level MOVES holds the union; legal_moves dispatches by phase.
MOVES = {
    "ready": {"args": [], "apply": _ready},
    "act":   {"args": [], "apply": _act},
}

def end_if(state, ctx):
    if state["winner"] != None:
        return {"winner": state["winner"]}
    return None

def legal_moves(state, ctx):
    if ctx.phase == "setup":
        return [{"move": "ready", "args": []}]
    return [{"move": "act", "args": []}]
`

func TestPhases_SetupThenPlay_TransitionsAndWins(t *testing.T) {
	spec, err := starlarkgame.LoadSpec(phasedSpec)
	if err != nil {
		t.Fatalf("LoadSpec: %v", err)
	}
	if err := starlarkgame.Validate(context.Background(), spec); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if spec.StartPhase != "setup" {
		t.Fatalf("StartPhase = %q, want setup", spec.StartPhase)
	}

	mgr := match.NewManager(storage.NewMemory())
	mgr.MustRegister(starlarkgame.BuildCoreGame(spec))

	mid, _ := mgr.Create("phased", match.CreateOptions{NumPlayers: 2})
	j0, _ := mgr.Join(mid, "p0", match.JoinOptions{PlayerID: "0"})
	j1, _ := mgr.Join(mid, "p1", match.JoinOptions{PlayerID: "1"})

	// Game opens in the "setup" phase.
	m, _ := mgr.State(mid)
	if m.State.Ctx.Phase != "setup" {
		t.Fatalf("opening phase = %q, want setup", m.State.Ctx.Phase)
	}

	// `act` must be rejected during setup — phase-scoped moves only.
	if _, err := mgr.Move(mid, j0.PlayerID, j0.PlayerCredentials, "act", nil); err == nil {
		t.Fatalf("expected act to be rejected during setup phase")
	}

	if _, err := mgr.Move(mid, j0.PlayerID, j0.PlayerCredentials, "ready", nil); err != nil {
		t.Fatalf("P0 ready: %v", err)
	}
	m, _ = mgr.State(mid)
	if m.State.Ctx.Phase != "setup" {
		t.Errorf("after P0 ready phase = %q, want still setup", m.State.Ctx.Phase)
	}

	if _, err := mgr.Move(mid, j1.PlayerID, j1.PlayerCredentials, "ready", nil); err != nil {
		t.Fatalf("P1 ready: %v", err)
	}
	m, _ = mgr.State(mid)
	if m.State.Ctx.Phase != "play" {
		t.Fatalf("after both ready phase = %q, want play", m.State.Ctx.Phase)
	}

	if _, err := mgr.Move(mid, j0.PlayerID, j0.PlayerCredentials, "act", nil); err != nil {
		t.Fatalf("P0 act: %v", err)
	}
	m, _ = mgr.State(mid)
	if m.State.Ctx.Gameover == nil {
		t.Fatalf("expected gameover after act, got %#v", m.State.Ctx)
	}
	go_ := m.State.Ctx.Gameover.(map[string]any)
	if go_["winner"] != "0" {
		t.Errorf("winner = %v, want 0", go_["winner"])
	}
}

func TestPhases_RejectMultipleStartPhases(t *testing.T) {
	_, err := starlarkgame.LoadSpec(`
META = {"name":"two-starts","min_players":2,"max_players":2}
def setup(ctx): return {}
def _a(state, ctx): return state
def _b(state, ctx): return state
PHASES = {
    "p1": {"moves": {"a": {"args": [], "apply": _a}}, "start": True},
    "p2": {"moves": {"b": {"args": [], "apply": _b}}, "start": True},
}
MOVES = {"a": {"args": [], "apply": _a}, "b": {"args": [], "apply": _b}}
def end_if(state, ctx): return None
def legal_moves(state, ctx): return []
`)
	if err == nil {
		t.Fatalf("expected error on two start phases")
	}
}

func TestPhases_RejectMissingStart(t *testing.T) {
	_, err := starlarkgame.LoadSpec(`
META = {"name":"no-start","min_players":2,"max_players":2}
def setup(ctx): return {}
def _a(state, ctx): return state
PHASES = {"p1": {"moves": {"a": {"args": [], "apply": _a}}}}
MOVES = {"a": {"args": [], "apply": _a}}
def end_if(state, ctx): return None
def legal_moves(state, ctx): return []
`)
	if err == nil {
		t.Fatalf("expected error on missing start phase")
	}
}

func TestPhases_PhasesAbsent_BackwardCompat(t *testing.T) {
	spec, err := starlarkgame.LoadSpec(`
META = {"name":"flat","min_players":2,"max_players":2}
def setup(ctx): return {"n": 0}
def _bump(state, ctx): return {"n": state["n"] + 1}
MOVES = {"bump": {"args": [], "apply": _bump}}
def end_if(state, ctx): return None
def legal_moves(state, ctx): return [{"move":"bump","args":[]}]
`)
	if err != nil {
		t.Fatalf("LoadSpec: %v", err)
	}
	if len(spec.Phases) != 0 || spec.StartPhase != "" {
		t.Errorf("phaseless spec leaked phase state: phases=%v start=%q", spec.Phases, spec.StartPhase)
	}
	g := starlarkgame.BuildCoreGame(spec)
	if g.Phases != nil {
		t.Errorf("BuildCoreGame populated Phases for a phaseless spec: %v", g.Phases)
	}
}
