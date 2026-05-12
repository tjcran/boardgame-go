package testhelpers_test

import (
	"testing"

	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/testhelpers"
)

type dnd struct{ Roll int }

func diceGame(plug core.Plugin) *core.Game {
	return &core.Game{
		Name:       "dnd",
		MinPlayers: 1,
		MaxPlayers: 1,
		Setup:      func(_ core.Ctx, _ any) core.G { return &dnd{} },
		Plugins:    []core.Plugin{plug},
		Moves: map[string]any{
			"roll": core.MoveFn(func(mc *core.MoveContext, _ ...any) (core.G, error) {
				return &dnd{Roll: mc.Random.D6().(int)}, nil
			}),
		},
		Turn: &core.TurnConfig{MinMoves: 1, MaxMoves: 1},
	}
}

func TestMockRandomOverridesD6(t *testing.T) {
	mock := testhelpers.MockRandom{
		D6Fn: func(_ ...int) any { return 6 },
	}
	g := diceGame(mock)
	s := core.NewMatch(g, 0, nil)
	s, err := core.Apply(g, s, core.MoveRequest{PlayerID: "0", Move: "roll"})
	if err != nil {
		t.Fatalf("roll: %v", err)
	}
	if s.G.(*dnd).Roll != 6 {
		t.Fatalf("expected mocked roll 6, got %d", s.G.(*dnd).Roll)
	}
}

func TestScenarioInjectsCustomG(t *testing.T) {
	type tttState struct {
		Cells [9]string
	}
	g := &core.Game{
		Name:       "scenario-game",
		MinPlayers: 2,
		MaxPlayers: 2,
		Setup:      func(_ core.Ctx, _ any) core.G { return &tttState{} },
		Moves: map[string]any{
			"noop": core.MoveFn(func(mc *core.MoveContext, _ ...any) (core.G, error) { return mc.G, nil }),
		},
		Turn: &core.TurnConfig{MinMoves: 1, MaxMoves: 1},
	}
	custom := &tttState{Cells: [9]string{"X", "O", "X", "O", "O", "", "", "", ""}}
	s := testhelpers.Scenario(g, custom, testhelpers.CtxOverrides{
		CurrentPlayer: "1",
		Turn:          7,
	})
	if s.G.(*tttState).Cells[0] != "X" {
		t.Fatalf("expected custom G to be used, got %+v", s.G)
	}
	if s.Ctx.CurrentPlayer != "1" || s.Ctx.Turn != 7 {
		t.Fatalf("expected overrides applied, got %+v", s.Ctx)
	}
}
