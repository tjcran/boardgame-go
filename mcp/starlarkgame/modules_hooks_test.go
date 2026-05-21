package starlarkgame

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/match"
	"github.com/tjcran/boardgame-go/storage"
)

// 1-player spec: 2-card deck, empty hand, a board creature, a graveyard.
// "slay" moves the creature to grave and publishes "died"; a HOOKS handler
// on "died" draws a card to hand. After one slay: hand=1, deck=1, grave=1.
const hooksGameSpec = `
META = {"name": "hooksgame", "min_players": 1, "max_players": 1}
MODULES = ["ccg"]

def setup(ctx):
    ctx.modules.ccg.new_zone(name="deck", ordered=True)
    ctx.modules.ccg.new_zone(name="hand", ordered=False)
    ctx.modules.ccg.new_zone(name="board", ordered=False)
    ctx.modules.ccg.new_zone(name="grave", ordered=False)
    for i in range(2):
        c = ctx.modules.ccg.new_entity(type="card", owner="0")
        ctx.modules.ccg.move_to(entity=c, zone="deck")
    cr = ctx.modules.ccg.new_entity(type="creature", owner="0")
    ctx.modules.ccg.move_to(entity=cr, zone="board")
    return {"creature": cr}

def end_if(state, ctx): return None
def legal_moves(state, ctx): return [{"name": "slay", "args": []}]

def on_died(event, ctx):
    top = ctx.modules.ccg.draw(zone="deck", n=1)
    ctx.modules.ccg.move_to(entity=top[0], zone="hand")

HOOKS = {"died": on_died}

def slay(state, ctx):
    cr = state["creature"]
    ctx.modules.ccg.move_to(entity=cr, zone="grave")
    ctx.modules.ccg.publish(type="died", source=cr)
    return state

MOVES = {"slay": {"apply": slay}}
`

func TestHooksGame_SlayDrawsAndReplays(t *testing.T) {
	spec, err := LoadSpec(hooksGameSpec)
	if err != nil {
		t.Fatalf("LoadSpec: %v", err)
	}
	if err := Validate(context.Background(), spec); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	g := BuildCoreGame(spec)
	mgr := match.NewManager(storage.NewMemory())
	mgr.MustRegister(g)
	id, _ := mgr.Create("hooksgame", match.CreateOptions{NumPlayers: 1})
	jr, _ := mgr.Join(id, "p", match.JoinOptions{})

	if _, err := mgr.MoveReq(id, jr.PlayerID, jr.PlayerCredentials, core.MoveRequest{Move: "slay"}); err != nil {
		t.Fatalf("slay: %v", err)
	}
	m, _ := mgr.State(id)
	sg := m.State.G.(*StarlarkG)
	cs := ccgStateOf(t, sg)
	if cs.Size("hand") != 1 || cs.Size("deck") != 1 || cs.Size("grave") != 1 {
		t.Fatalf("after slay: hand=%d deck=%d grave=%d, want 1/1/1", cs.Size("hand"), cs.Size("deck"), cs.Size("grave"))
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
