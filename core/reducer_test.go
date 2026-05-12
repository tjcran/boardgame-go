package core

import (
	"errors"
	"testing"
)

// counterGame is a tiny test fixture: players take turns incrementing or
// decrementing a counter. First to reach 3 wins. Exercises Apply/EndIf/
// turn rotation in isolation from the rest of the framework.
func counterGame() *Game {
	type state struct{ Count int }
	return &Game{
		Name:       "counter",
		MinPlayers: 2,
		MaxPlayers: 2,
		Setup:      func(_ Ctx, _ any) G { return &state{} },
		Moves: map[string]any{
			"inc": MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
				s := mc.G.(*state)
				return &state{Count: s.Count + 1}, nil
			}),
			"dec": MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
				s := mc.G.(*state)
				return &state{Count: s.Count - 1}, nil
			}),
			"refuse": MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
				return mc.G, ErrInvalidMove
			}),
		},
		EndIf: func(mc *MoveContext) any {
			s := mc.G.(*state)
			if s.Count >= 3 {
				return map[string]any{"winner": mc.Ctx.CurrentPlayer}
			}
			return nil
		},
		Turn: &TurnConfig{MinMoves: 1, MaxMoves: 1},
	}
}

func TestNewMatchInitialState(t *testing.T) {
	g := counterGame()
	s := NewMatch(g, 0, nil)
	if s.Ctx.NumPlayers != 2 {
		t.Fatalf("expected 2 players, got %d", s.Ctx.NumPlayers)
	}
	if s.Ctx.CurrentPlayer != "0" || s.Ctx.Turn != 1 {
		t.Fatalf("bad initial ctx: %+v", s.Ctx)
	}
}

func TestApplyAdvancesTurn(t *testing.T) {
	g := counterGame()
	s := NewMatch(g, 0, nil)
	next, err := Apply(g, s, MoveRequest{PlayerID: "0", Move: "inc"})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if next.Ctx.CurrentPlayer != "1" {
		t.Fatalf("expected p1's turn, got %s", next.Ctx.CurrentPlayer)
	}
	if next.Ctx.Turn != 2 {
		t.Fatalf("expected turn 2, got %d", next.Ctx.Turn)
	}
}

func TestApplyRejectsWrongPlayer(t *testing.T) {
	g := counterGame()
	s := NewMatch(g, 0, nil)
	_, err := Apply(g, s, MoveRequest{PlayerID: "1", Move: "inc"})
	if !errors.Is(err, ErrWrongPlayer) {
		t.Fatalf("expected ErrWrongPlayer, got %v", err)
	}
}

func TestApplyRejectsUnknownMove(t *testing.T) {
	g := counterGame()
	s := NewMatch(g, 0, nil)
	_, err := Apply(g, s, MoveRequest{PlayerID: "0", Move: "spin"})
	if !errors.Is(err, ErrUnknownMove) {
		t.Fatalf("expected ErrUnknownMove, got %v", err)
	}
}

func TestApplyPropagatesInvalidMove(t *testing.T) {
	g := counterGame()
	s := NewMatch(g, 0, nil)
	_, err := Apply(g, s, MoveRequest{PlayerID: "0", Move: "refuse"})
	if !errors.Is(err, ErrInvalidMove) {
		t.Fatalf("expected ErrInvalidMove, got %v", err)
	}
}

func TestEndIfWritesGameoverAndFreezes(t *testing.T) {
	g := counterGame()
	s := NewMatch(g, 0, nil)
	var err error
	for _, who := range []string{"0", "1", "0"} {
		s, err = Apply(g, s, MoveRequest{PlayerID: who, Move: "inc"})
		if err != nil {
			t.Fatalf("apply: %v", err)
		}
	}
	if s.Ctx.Gameover == nil {
		t.Fatalf("expected gameover, got %+v", s.Ctx)
	}
	if m, ok := s.Ctx.Gameover.(map[string]any); !ok || m["winner"] != "0" {
		t.Fatalf("expected winner=0, got %v", s.Ctx.Gameover)
	}
	_, err = Apply(g, s, MoveRequest{PlayerID: "1", Move: "inc"})
	if !errors.Is(err, ErrGameOver) {
		t.Fatalf("expected ErrGameOver, got %v", err)
	}
}
