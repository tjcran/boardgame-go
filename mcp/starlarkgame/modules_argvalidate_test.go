package starlarkgame

import (
	"context"
	"testing"

	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/match"
	"github.com/tjcran/boardgame-go/storage"
)

const handArgSpec = `
META = {"name": "handarg", "min_players": 1, "max_players": 1}
MODULES = ["ccg"]

def setup(ctx):
    ctx.modules.ccg.new_zone(name="deck", ordered=True)
    ctx.modules.ccg.new_zone(name="hand", ordered=False)
    ctx.modules.ccg.new_zone(name="board", ordered=False)
    a = ctx.modules.ccg.new_entity(type="card", owner="0")
    b = ctx.modules.ccg.new_entity(type="card", owner="0")
    ctx.modules.ccg.move_to(entity=a, zone="deck")
    ctx.modules.ccg.move_to(entity=b, zone="hand")
    return {}

def end_if(state, ctx): return None
def legal_moves(state, ctx): return []

def play_card(state, ctx, card):
    ctx.modules.ccg.move_to(entity=card, zone="board")
    return state

MOVES = {"play_card": {"apply": play_card, "args": [{"name": "card", "type": "entity", "zone": "hand"}]}}
`

func TestHandArg_RejectsOutOfZoneEntity(t *testing.T) {
	spec, err := LoadSpec(handArgSpec)
	if err != nil {
		t.Fatalf("LoadSpec: %v", err)
	}
	if err := Validate(context.Background(), spec); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	g := BuildCoreGame(spec)
	mgr := match.NewManager(storage.NewMemory())
	mgr.MustRegister(g)
	id, _ := mgr.Create("handarg", match.CreateOptions{NumPlayers: 1})
	jr, _ := mgr.Join(id, "p", match.JoinOptions{})

	if _, err := mgr.MoveReq(id, jr.PlayerID, jr.PlayerCredentials, core.MoveRequest{Move: "play_card", Args: []any{"ent:1"}}); err == nil {
		t.Fatal("expected play_card(ent:1) to be rejected (not in hand)")
	}
	if _, err := mgr.MoveReq(id, jr.PlayerID, jr.PlayerCredentials, core.MoveRequest{Move: "play_card", Args: []any{"ent:2"}}); err != nil {
		t.Fatalf("play_card(ent:2) should succeed: %v", err)
	}
}
