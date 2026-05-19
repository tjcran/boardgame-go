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

func TestLogEntryStampsStage(t *testing.T) {
	// A two-player game with a stage. Player 0 enters the "discard" stage
	// via SetActivePlayers, then dispatches a move. The resulting log
	// entry should carry Stage="discard".
	g := &Game{
		Name: "stage-log-test", MinPlayers: 2, MaxPlayers: 2,
		Setup: func(_ Ctx, _ any) G { return map[string]int{"n": 0} },
		Moves: map[string]any{
			"act": MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
				return mc.G, nil
			}),
			"enterStage": MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
				mc.Events.SetActivePlayers(ActivePlayersConfig{
					Value: map[string]string{"0": "discard"},
				})
				return mc.G, nil
			}),
		},
		Turn: &TurnConfig{MinMoves: 1, MaxMoves: 8},
	}
	s := NewMatch(g, 0, nil)

	// Move 1: enter the stage.
	s, err := Apply(g, s, MoveRequest{PlayerID: "0", Move: "enterStage"})
	if err != nil {
		t.Fatalf("enterStage: %v", err)
	}
	// Move 2: act while in stage "discard".
	s, err = Apply(g, s, MoveRequest{PlayerID: "0", Move: "act"})
	if err != nil {
		t.Fatalf("act-in-stage: %v", err)
	}
	// The second move's log entry should carry Stage="discard".
	if last := s.Log[len(s.Log)-1]; last.Stage != "discard" {
		t.Fatalf("in-stage move log entry Stage = %q, want %q", last.Stage, "discard")
	}
}

func TestLogEntryStageEmptyOutsideStage(t *testing.T) {
	g := &Game{
		Name: "stage-log-test-2", MinPlayers: 2, MaxPlayers: 2,
		Setup: func(_ Ctx, _ any) G { return map[string]int{} },
		Moves: map[string]any{
			"act": MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
				return mc.G, nil
			}),
		},
		Turn: &TurnConfig{MinMoves: 1, MaxMoves: 1},
	}
	s := NewMatch(g, 0, nil)
	s, err := Apply(g, s, MoveRequest{PlayerID: "0", Move: "act"})
	if err != nil {
		t.Fatalf("act: %v", err)
	}
	if last := s.Log[len(s.Log)-1]; last.Stage != "" {
		t.Fatalf("outside-stage move log entry Stage = %q, want empty", last.Stage)
	}
}
