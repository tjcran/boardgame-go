package core

import (
	"errors"
	"testing"
)

// counterGame is a tiny test fixture: players take turns incrementing or
// decrementing a counter. First to reach 3 wins.
func counterGame() *Game {
	type state struct {
		Count int `json:"count"`
	}
	return &Game{
		Name:       "counter",
		MinPlayers: 2,
		MaxPlayers: 2,
		Setup:      func(n int) G { return &state{} },
		Moves: map[string]MoveFn{
			"inc": func(g G, _ Ctx, _ ...any) (G, error) {
				s := g.(*state)
				return &state{Count: s.Count + 1}, nil
			},
			"dec": func(g G, _ Ctx, _ ...any) (G, error) {
				s := g.(*state)
				return &state{Count: s.Count - 1}, nil
			},
			"refuse": func(g G, _ Ctx, _ ...any) (G, error) {
				return g, ErrInvalidMove
			},
		},
		EndIf: func(g G, ctx Ctx) (bool, string, bool) {
			s := g.(*state)
			if s.Count >= 3 {
				return true, ctx.CurrentPlayer, false
			}
			return false, "", false
		},
	}
}

func TestNewMatchInitialState(t *testing.T) {
	g := counterGame()
	s := NewMatch(g, 0)
	if s.Ctx.NumPlayers != 2 {
		t.Fatalf("expected 2 players, got %d", s.Ctx.NumPlayers)
	}
	if s.Ctx.CurrentPlayer != "0" || s.Ctx.Turn != 1 {
		t.Fatalf("bad initial ctx: %+v", s.Ctx)
	}
}

func TestApplyAdvancesTurn(t *testing.T) {
	g := counterGame()
	s := NewMatch(g, 0)
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
	s := NewMatch(g, 0)
	_, err := Apply(g, s, MoveRequest{PlayerID: "1", Move: "inc"})
	if !errors.Is(err, ErrWrongPlayer) {
		t.Fatalf("expected ErrWrongPlayer, got %v", err)
	}
}

func TestApplyRejectsUnknownMove(t *testing.T) {
	g := counterGame()
	s := NewMatch(g, 0)
	_, err := Apply(g, s, MoveRequest{PlayerID: "0", Move: "spin"})
	if !errors.Is(err, ErrUnknownMove) {
		t.Fatalf("expected ErrUnknownMove, got %v", err)
	}
}

func TestApplyPropagatesInvalidMove(t *testing.T) {
	g := counterGame()
	s := NewMatch(g, 0)
	_, err := Apply(g, s, MoveRequest{PlayerID: "0", Move: "refuse"})
	if !errors.Is(err, ErrInvalidMove) {
		t.Fatalf("expected ErrInvalidMove, got %v", err)
	}
}

func TestEndIfDeclaresWinnerAndFreezes(t *testing.T) {
	g := counterGame()
	s := NewMatch(g, 0)
	var err error
	for _, who := range []string{"0", "1", "0"} { // 3 incs, last by player 0
		s, err = Apply(g, s, MoveRequest{PlayerID: who, Move: "inc"})
		if err != nil {
			t.Fatalf("apply: %v", err)
		}
	}
	if !s.Ctx.GameOver || s.Ctx.Winner != "0" {
		t.Fatalf("expected game over with winner 0: %+v", s.Ctx)
	}
	// further moves rejected
	_, err = Apply(g, s, MoveRequest{PlayerID: "1", Move: "inc"})
	if !errors.Is(err, ErrGameOver) {
		t.Fatalf("expected ErrGameOver, got %v", err)
	}
}
