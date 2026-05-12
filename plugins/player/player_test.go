package player_test

import (
	"testing"

	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/plugins/player"
)

type record struct {
	HP int `json:"hp"`
}

// twoPlayerHPGame is a fixture exercising the per-player plugin: each turn
// the current player attacks the opponent by decrementing their HP.
func twoPlayerHPGame() *core.Game {
	return &core.Game{
		Name:       "hp",
		MinPlayers: 2,
		MaxPlayers: 2,
		Setup:      func(_ core.Ctx, _ any) core.G { return struct{}{} },
		Plugins: []core.Plugin{
			player.New(player.Options{
				PlayerSetup: func(_ string) any { return &record{HP: 10} },
			}),
		},
		Moves: map[string]any{
			"attack": core.MoveFn(func(mc *core.MoveContext, _ ...any) (core.G, error) {
				api := mc.Plugins[player.PluginName].(*player.API)
				op := api.Opponent.Get().(*record)
				api.Opponent.Set(&record{HP: op.HP - 1})
				return mc.G, nil
			}),
		},
		Turn: &core.TurnConfig{MinMoves: 1, MaxMoves: 1},
	}
}

// pluginAPI is a tiny helper to peek into the plugin's per-seat API outside
// a running move — useful for assertions in tests.
func pluginAPI(g *core.Game, s core.State, pid string) *player.API {
	var p *player.Plugin
	for _, plg := range g.Plugins {
		if pp, ok := plg.(*player.Plugin); ok {
			p = pp
			break
		}
	}
	return p.API(s.Plugins[player.PluginName], s.G, s.Ctx, pid, g).(*player.API)
}

func TestPluginPlayerSetupCreatesPerSeatRecord(t *testing.T) {
	g := twoPlayerHPGame()
	s := core.NewMatch(g, 0, nil)
	if s.Plugins[player.PluginName] == nil {
		t.Fatal("player plugin data missing")
	}
	api := pluginAPI(g, s, "0")
	rec := api.Get().(*record)
	if rec.HP != 10 {
		t.Fatalf("expected initial HP=10, got %d", rec.HP)
	}
}

func TestPluginPlayerOpponentSetMutatesAcrossMove(t *testing.T) {
	g := twoPlayerHPGame()
	s := core.NewMatch(g, 0, nil)
	s, err := core.Apply(g, s, core.MoveRequest{PlayerID: "0", Move: "attack"})
	if err != nil {
		t.Fatalf("attack: %v", err)
	}
	// From p0's POV, opponent is p1 — should now have HP=9.
	op := pluginAPI(g, s, "0").Opponent.Get().(*record)
	if op.HP != 9 {
		t.Fatalf("expected opponent HP=9, got %d", op.HP)
	}
}

func TestPluginPlayerViewHidesOpponentByDefault(t *testing.T) {
	g := twoPlayerHPGame()
	s := core.NewMatch(g, 0, nil)

	view := core.PlayerView(g, s, "0")
	api := pluginAPI(g, view, "0")
	if api.Get() == nil {
		t.Fatal("own record should be visible in PlayerView")
	}
	if api.Opponent != nil && api.Opponent.Get() != nil {
		t.Fatalf("opponent record should be hidden, got %+v", api.Opponent.Get())
	}
}
