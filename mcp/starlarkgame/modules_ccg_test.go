package starlarkgame

import (
	"context"
	"testing"

	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/modules/ccg"
)

// A minimal ccg-backed spec: setup builds a deck of 3 cards and a hand;
// the "draw" move moves the top deck card into the hand.
const miniCCGSpec = `
META = {"name": "mini-ccg", "min_players": 1, "max_players": 1}
MODULES = ["ccg"]

def setup(ctx):
    ctx.modules.ccg.new_zone(name="deck", ordered=True)
    ctx.modules.ccg.new_zone(name="hand", ordered=False)
    for i in range(3):
        e = ctx.modules.ccg.new_entity(type="card", owner="0")
        ctx.modules.ccg.move_to(entity=e, zone="deck")
    return {}

def end_if(state, ctx): return None
def legal_moves(state, ctx): return [{"name": "draw", "args": []}]

def draw(state, ctx):
    drawn = ctx.modules.ccg.draw(zone="deck", n=1)
    ctx.modules.ccg.move_to(entity=drawn[0], zone="hand")
    return state

MOVES = {"draw": {"apply": draw}}
`

func coreCtx(n int) core.Ctx { return core.Ctx{NumPlayers: n} }

func ccgStateOf(t *testing.T, sg *StarlarkG) *ccg.State {
	t.Helper()
	st, ok := sg.Modules["ccg"].(*ccg.State)
	if !ok {
		t.Fatalf("modules[ccg] is %T, want *ccg.State", sg.Modules["ccg"])
	}
	return st
}

func TestMiniCCG_SetupAndDraw(t *testing.T) {
	spec, err := LoadSpec(miniCCGSpec)
	if err != nil {
		t.Fatalf("LoadSpec: %v", err)
	}
	if err := Validate(context.Background(), spec); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	g := BuildCoreGame(spec)

	sg := g.Setup(coreCtx(1), nil).(*StarlarkG)
	st := ccgStateOf(t, sg)
	if st.Size("deck") != 3 || st.Size("hand") != 0 {
		t.Fatalf("after setup: deck=%d hand=%d, want 3/0", st.Size("deck"), st.Size("hand"))
	}
}
