package starlarkgame

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/match"
	"github.com/tjcran/boardgame-go/storage"
)

// A 1-player hex-skirmish spec: setup makes a 6x6 hex board and places
// unit 1 at (0,0). The "advance" move steps unit 1 to its first neighbor.
const miniTabletopSpec = `
META = {"name": "mini-tabletop", "min_players": 1, "max_players": 1}
MODULES = ["tabletop"]

def setup(ctx):
    ctx.modules.tabletop.new_board(kind="hex", w=6, h=6)
    ctx.modules.tabletop.place(unit=1, x=0, y=0)
    return {"steps": 0}

def end_if(state, ctx): return None
def legal_moves(state, ctx): return [{"name": "advance", "args": []}]

def advance(state, ctx):
    here = ctx.modules.tabletop.position_of(unit=1)
    ns = ctx.modules.tabletop.neighbors(x=here[0], y=here[1])
    dest = ns[0]
    ctx.modules.tabletop.move(unit=1, x=dest[0], y=dest[1])
    return {"steps": state["steps"] + 1}

MOVES = {"advance": {"apply": advance}}
`

func TestMiniTabletop_SetupRunReplay(t *testing.T) {
	spec, err := LoadSpec(miniTabletopSpec)
	if err != nil {
		t.Fatalf("LoadSpec: %v", err)
	}
	if err := Validate(context.Background(), spec); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	g := BuildCoreGame(spec)

	mgr := match.NewManager(storage.NewMemory())
	mgr.MustRegister(g)
	id, err := mgr.Create("mini-tabletop", match.CreateOptions{NumPlayers: 1})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	jr, err := mgr.Join(id, "p", match.JoinOptions{})
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	for i := 0; i < 2; i++ {
		if _, err := mgr.MoveReq(id, jr.PlayerID, jr.PlayerCredentials, core.MoveRequest{Move: "advance"}); err != nil {
			t.Fatalf("advance %d: %v", i, err)
		}
	}

	st, _ := mgr.State(id)
	sg := st.State.G.(*StarlarkG)
	if sg.Data["steps"] != int64(2) && sg.Data["steps"] != float64(2) {
		t.Fatalf("steps = %v, want 2", sg.Data["steps"])
	}

	replayed, err := core.Replay(g, st.State.Log, 1, nil)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	orig, _ := json.Marshal(st.State.G)
	repl, _ := json.Marshal(replayed.G)
	if string(orig) != string(repl) {
		t.Fatalf("replay mismatch:\n orig=%s\n repl=%s", orig, repl)
	}
}
