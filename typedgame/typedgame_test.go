package typedgame_test

import (
	"testing"

	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/typedgame"
)

// counterState exercises the generic shell: an int counter incremented by
// the only move. No runtime assertions in the move body — the compiler
// already knows mc.G is *counterState.
type counterState struct{ Score int }

func TestTypedGameBuildsAndRuns(t *testing.T) {
	g := typedgame.Game[*counterState]{
		Name:       "counter",
		MinPlayers: 1,
		MaxPlayers: 1,
		Setup:      func(_ core.Ctx, _ any) *counterState { return &counterState{} },
		Moves: typedgame.Moves[*counterState]{
			"inc": typedgame.MoveFn[*counterState](func(mc *typedgame.Context[*counterState], _ ...any) (*counterState, error) {
				// mc.G is *counterState — no .(*X) anywhere.
				return &counterState{Score: mc.G.Score + 1}, nil
			}),
		},
		EndIf: func(mc *typedgame.Context[*counterState]) any {
			if mc.G.Score >= 3 {
				return map[string]any{"winner": mc.Ctx.CurrentPlayer}
			}
			return nil
		},
		Turn: &typedgame.TurnConfig[*counterState]{MinMoves: 1, MaxMoves: 1},
	}.Build()

	s := core.NewMatch(g, 0, nil)
	for i := 0; i < 3; i++ {
		next, err := core.Apply(g, s, core.MoveRequest{PlayerID: "0", Move: "inc"})
		if err != nil {
			t.Fatalf("inc %d: %v", i, err)
		}
		s = next
	}
	if s.Ctx.Gameover == nil {
		t.Fatalf("expected gameover after 3 incs, got %+v", s.Ctx)
	}
	if m, _ := s.Ctx.Gameover.(map[string]any); m["winner"] != "0" {
		t.Fatalf("expected winner=0, got %v", s.Ctx.Gameover)
	}
}

func TestTypedGameHooksReceiveTypedG(t *testing.T) {
	var sawOnBegin int
	g := typedgame.Game[*counterState]{
		Name:       "counter",
		MinPlayers: 1,
		MaxPlayers: 1,
		Setup:      func(_ core.Ctx, _ any) *counterState { return &counterState{Score: 7} },
		Moves: typedgame.Moves[*counterState]{
			"x": typedgame.MoveFn[*counterState](func(mc *typedgame.Context[*counterState], _ ...any) (*counterState, error) {
				return mc.G, nil
			}),
		},
		Turn: &typedgame.TurnConfig[*counterState]{
			OnBegin: func(mc *typedgame.Context[*counterState]) *counterState {
				sawOnBegin = mc.G.Score
				return mc.G
			},
			MinMoves: 1, MaxMoves: 1,
		},
	}.Build()

	core.NewMatch(g, 0, nil)
	if sawOnBegin != 7 {
		t.Fatalf("expected OnBegin to see typed G.Score=7, got %d", sawOnBegin)
	}
}

func TestTypedMoveLongFormCarriesFlags(t *testing.T) {
	g := typedgame.Game[*counterState]{
		Name:       "counter",
		MinPlayers: 1,
		MaxPlayers: 1,
		Setup:      func(_ core.Ctx, _ any) *counterState { return &counterState{} },
		Moves: typedgame.Moves[*counterState]{
			"inc": typedgame.Move[*counterState]{
				Move: func(mc *typedgame.Context[*counterState], _ ...any) (*counterState, error) {
					return &counterState{Score: mc.G.Score + 1}, nil
				},
				Undoable: false,
				NoLimit:  true,
			},
		},
		Turn: &typedgame.TurnConfig[*counterState]{MinMoves: 1, MaxMoves: 1},
	}.Build()

	s := core.NewMatch(g, 0, nil)
	// NoLimit means this move shouldn't auto-end the turn even though
	// MaxMoves=1; current player should still be "0" after the call.
	s, err := core.Apply(g, s, core.MoveRequest{PlayerID: "0", Move: "inc"})
	if err != nil {
		t.Fatalf("inc: %v", err)
	}
	if s.Ctx.CurrentPlayer != "0" {
		t.Fatalf("expected NoLimit to prevent auto-end-turn, got current=%s", s.Ctx.CurrentPlayer)
	}
}
