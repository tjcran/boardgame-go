package starlarkgame

import (
	"context"
	"testing"

	"github.com/tjcran/boardgame-go/modules/ccg"
)

func TestHooks_FireOnPublishMutateState(t *testing.T) {
	spec, err := LoadSpec(`
META = {"name":"h","min_players":1,"max_players":1}
MODULES = ["ccg"]
def setup(ctx):
    ctx.modules.ccg.new_zone(name="deck", ordered=True)
    ctx.modules.ccg.new_zone(name="hand", ordered=False)
    c = ctx.modules.ccg.new_entity(type="card", owner="0")
    ctx.modules.ccg.move_to(entity=c, zone="deck")
    return {}
def end_if(state, ctx): return None
def legal_moves(state, ctx): return []
def on_bell(event, ctx):
    top = ctx.modules.ccg.draw(zone="deck", n=1)
    ctx.modules.ccg.move_to(entity=top[0], zone="hand")
HOOKS = {"bell": on_bell}
def ring(state, ctx):
    ctx.modules.ccg.publish(type="bell")
    return state
MOVES = {"ring": {"apply": ring}}
`)
	if err != nil {
		t.Fatalf("LoadSpec: %v", err)
	}
	g := BuildCoreGame(spec)
	sg := g.Setup(coreCtx(1), nil).(*StarlarkG)
	st := sg.Modules["ccg"].(*ccg.State)
	if st.Size("deck") != 1 || st.Size("hand") != 0 {
		t.Fatalf("setup: deck=%d hand=%d", st.Size("deck"), st.Size("hand"))
	}
	bc := &BridgeCtx{NumPlayers: 1, Modules: sg.Modules}
	if _, err := spec.CallMove(context.Background(), bc, "ring", sg.Data, nil); err != nil {
		t.Fatalf("ring move: %v", err)
	}
	if st.Size("deck") != 0 || st.Size("hand") != 1 {
		t.Fatalf("after hook: deck=%d hand=%d, want 0/1", st.Size("deck"), st.Size("hand"))
	}
}

func TestHooks_HandlerErrorRejectsMove(t *testing.T) {
	spec, err := LoadSpec(`
META = {"name":"h","min_players":1,"max_players":1}
MODULES = ["ccg"]
def setup(ctx): return {}
def end_if(state, ctx): return None
def legal_moves(state, ctx): return []
def on_boom(event, ctx):
    fail("kaboom")
HOOKS = {"boom": on_boom}
def trigger(state, ctx):
    ctx.modules.ccg.publish(type="boom")
    return state
MOVES = {"trigger": {"apply": trigger}}
`)
	if err != nil {
		t.Fatalf("LoadSpec: %v", err)
	}
	g := BuildCoreGame(spec)
	sg := g.Setup(coreCtx(1), nil).(*StarlarkG)
	bc := &BridgeCtx{NumPlayers: 1, Modules: sg.Modules}
	if _, err := spec.CallMove(context.Background(), bc, "trigger", sg.Data, nil); err == nil {
		t.Fatal("expected the raising hook to reject the move")
	}
}
