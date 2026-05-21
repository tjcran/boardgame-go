package starlarkgame

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/match"
	"github.com/tjcran/boardgame-go/storage"
)

// A 1-player mini auto-battler turn: setup gives the player 3 gold and a
// shop of 2 items (cost 1) drawn from a 3-item stock. "buy_first" buys the
// first slot item into hand and pays 1 gold.
const miniShopSpec = `
META = {"name": "mini-shop", "min_players": 1, "max_players": 1}
MODULES = ["ccg", "economy", "shop"]

def setup(ctx):
    p = ctx.modules.ccg.new_entity(type="player", owner="0")
    ctx.modules.economy.set(owner=p, kind="gold", cap=10, n=3)
    ctx.modules.ccg.new_zone(name="slots", ordered=False)
    ctx.modules.ccg.new_zone(name="stock", ordered=True)
    ctx.modules.ccg.new_zone(name="hand", ordered=False)
    for i in range(3):
        c = ctx.modules.ccg.new_entity(type="item", owner="")
        ctx.modules.ccg.move_to(entity=c, zone="stock")
    ctx.modules.shop.fill(slots="slots", stock="stock", size=2)
    return {"player": p}

def end_if(state, ctx): return None
def legal_moves(state, ctx): return [{"name": "buy_first", "args": []}]

def buy_first(state, ctx):
    p = state["player"]
    item = ctx.modules.ccg.members(zone="slots")[0]
    ctx.modules.economy.spend(owner=p, kind="gold", n=1)
    ctx.modules.shop.buy(slots="slots", item=item, dest="hand")
    return state

MOVES = {"buy_first": {"apply": buy_first}}
`

func matchState(mgr *match.Manager, id string) *storage.Match {
	m, _ := mgr.State(id)
	return m
}

func TestMiniShop_SetupBuyReplay(t *testing.T) {
	spec, err := LoadSpec(miniShopSpec)
	if err != nil {
		t.Fatalf("LoadSpec: %v", err)
	}
	if err := Validate(context.Background(), spec); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	g := BuildCoreGame(spec)

	mgr := match.NewManager(storage.NewMemory())
	mgr.MustRegister(g)
	id, err := mgr.Create("mini-shop", match.CreateOptions{NumPlayers: 1})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	jr, err := mgr.Join(id, "p", match.JoinOptions{})
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	if _, err := mgr.MoveReq(id, jr.PlayerID, jr.PlayerCredentials, core.MoveRequest{Move: "buy_first"}); err != nil {
		t.Fatalf("buy_first: %v", err)
	}

	replayed, err := core.Replay(g, matchState(mgr, id).State.Log, 1, nil)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	orig, _ := json.Marshal(matchState(mgr, id).State.G)
	repl, _ := json.Marshal(replayed.G)
	if string(orig) != string(repl) {
		t.Fatalf("replay mismatch:\n orig=%s\n repl=%s", orig, repl)
	}
}
