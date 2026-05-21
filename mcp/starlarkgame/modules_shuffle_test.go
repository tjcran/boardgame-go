package starlarkgame

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/match"
	"github.com/tjcran/boardgame-go/storage"
)

const shuffleSpec = `
META = {"name": "shuffler", "min_players": 1, "max_players": 1}
MODULES = ["ccg"]

def setup(ctx):
    ctx.modules.ccg.new_zone(name="deck", ordered=True)
    for i in range(8):
        c = ctx.modules.ccg.new_entity(type="card", owner="0")
        ctx.modules.ccg.move_to(entity=c, zone="deck")
    return {}

def end_if(state, ctx): return None
def legal_moves(state, ctx): return [{"name": "shuffle", "args": []}]

def shuffle(state, ctx):
    ctx.modules.ccg.shuffle(zone="deck")
    return state

MOVES = {"shuffle": {"apply": shuffle}}
`

func TestShuffle_MoveAndReplay(t *testing.T) {
	spec, err := LoadSpec(shuffleSpec)
	if err != nil {
		t.Fatalf("LoadSpec: %v", err)
	}
	if err := Validate(context.Background(), spec); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	g := BuildCoreGame(spec)
	mgr := match.NewManager(storage.NewMemory())
	mgr.MustRegister(g)
	id, _ := mgr.Create("shuffler", match.CreateOptions{NumPlayers: 1})
	jr, _ := mgr.Join(id, "p", match.JoinOptions{})

	if _, err := mgr.MoveReq(id, jr.PlayerID, jr.PlayerCredentials, core.MoveRequest{Move: "shuffle"}); err != nil {
		t.Fatalf("shuffle move: %v", err)
	}
	m, _ := mgr.State(id)
	cs := ccgStateOf(t, m.State.G.(*StarlarkG))
	if cs.Size("deck") != 8 {
		t.Fatalf("deck size changed: %d", cs.Size("deck"))
	}

	replayed, err := core.Replay(g, m.State.Log, 1, nil)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	orig, _ := json.Marshal(m.State.G)
	repl, _ := json.Marshal(replayed.G)
	if string(orig) != string(repl) {
		t.Fatalf("replay mismatch:\n orig=%s\n repl=%s", orig, repl)
	}
}
