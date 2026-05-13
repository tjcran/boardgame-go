package core

import (
	"testing"
)

// TestBGIO924_EndTurnNextSurvivesMaxMoves reproduces the BGIO bug
// (#924): events.endTurn({ next: '2' }) was overridden by the implicit
// MaxMoves auto-end-turn that fires later in the same Apply.
//
// Our reducer drains events before the auto-end check, and endTurn
// itself clears NumMoves to 0 — so when the MaxMoves check runs, it
// finds NumMoves=0 < MaxMoves and skips. The next override survives.
func TestBGIO924_EndTurnNextSurvivesMaxMoves(t *testing.T) {
	g := &Game{
		Name:       "next-survives",
		MinPlayers: 3,
		MaxPlayers: 3,
		Setup:      func(_ Ctx, _ any) G { return struct{}{} },
		Moves: map[string]any{
			"jumpTo2": MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
				mc.Events.EndTurn("2")
				return mc.G, nil
			}),
		},
		Turn: &TurnConfig{MinMoves: 1, MaxMoves: 1},
	}
	s := NewMatch(g, 0, nil)
	s, err := Apply(g, s, MoveRequest{PlayerID: "0", Move: "jumpTo2"})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if s.Ctx.CurrentPlayer != "2" {
		t.Fatalf("expected MaxMoves to NOT override events.EndTurn(next=2); got current=%s",
			s.Ctx.CurrentPlayer)
	}
}

// TestBGIO1048_CanUndoMatchesUndoBehaviour verifies CanUndo reports
// exactly when Undo would succeed.
func TestBGIO1048_CanUndoMatchesUndoBehaviour(t *testing.T) {
	g := multiMoveGame(false, true)
	s := NewMatch(g, 0, nil)

	if CanUndo(g, s) {
		t.Fatal("CanUndo should be false before any move")
	}
	s, _ = Apply(g, s, MoveRequest{PlayerID: "0", Move: "inc"})
	if !CanUndo(g, s) {
		t.Fatal("CanUndo should be true after an undoable move")
	}
	u, err := Undo(g, s)
	if err != nil {
		t.Fatalf("Undo: %v", err)
	}
	if CanUndo(g, u) {
		t.Fatal("CanUndo should be false after Undo emptied the stack")
	}
}

// TestBGIO1085_EventsRunMoveChainsThroughEngine verifies a move
// dispatching another move via Events.RunMove goes through validation
// and logging.
func TestBGIO1085_EventsRunMoveChainsThroughEngine(t *testing.T) {
	type S struct{ A, B int }
	g := &Game{
		Name:       "chain",
		MinPlayers: 1,
		MaxPlayers: 1,
		Setup:      func(_ Ctx, _ any) G { return &S{} },
		Moves: map[string]any{
			"alpha": MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
				mc.Events.RunMove("beta")
				return &S{A: mc.G.(*S).A + 1, B: mc.G.(*S).B}, nil
			}),
			"beta": MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
				return &S{A: mc.G.(*S).A, B: mc.G.(*S).B + 1}, nil
			}),
		},
	}
	s := NewMatch(g, 0, nil)
	s, err := Apply(g, s, MoveRequest{PlayerID: "0", Move: "alpha"})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	got := s.G.(*S)
	if got.A != 1 || got.B != 1 {
		t.Fatalf("expected A=1 B=1 after chained Events.RunMove, got %+v", got)
	}
	// Both entries should appear in the log: the original move and the chained one.
	gotKinds := []string{}
	for _, e := range s.Log {
		gotKinds = append(gotKinds, e.Kind)
	}
	if len(gotKinds) < 2 || gotKinds[0] != "move" || gotKinds[1] != "chained-move" {
		t.Fatalf("expected [move, chained-move] in log, got %v", gotKinds)
	}
}

// TestBGIO892_ReplayUntilStopsAtStepN verifies replay-to-N.
func TestBGIO892_ReplayUntilStopsAtStepN(t *testing.T) {
	g := counterGame()
	live := NewMatch(g, 0, nil)
	for _, who := range []string{"0", "1", "0"} {
		next, _ := Apply(g, live, MoveRequest{PlayerID: who, Move: "inc"})
		live = next
	}
	// Replay only the first two moves.
	partial, err := ReplayUntil(g, live.Log, 2, 0, nil)
	if err != nil {
		t.Fatalf("replay-until: %v", err)
	}
	if partial.Ctx.Turn != 3 {
		t.Fatalf("expected turn 3 after 2 moves, got %d", partial.Ctx.Turn)
	}
	if partial.Ctx.Gameover != nil {
		t.Fatalf("expected no gameover at step 2, got %v", partial.Ctx.Gameover)
	}
}

// TestBGIO478_ActivePlayersInOrderCyclesThroughList verifies the helper
// activates one player at a time.
func TestBGIO478_ActivePlayersInOrderCyclesThroughList(t *testing.T) {
	g := &Game{
		Name:       "in-order",
		MinPlayers: 3,
		MaxPlayers: 3,
		Setup:      func(_ Ctx, _ any) G { return struct{}{} },
		Moves: map[string]any{
			"queue": MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
				mc.Events.SetActivePlayers(
					ActivePlayersInOrder([]string{"1", "2"}, StageNull, 1, 1),
				)
				return mc.G, nil
			}),
			"act": MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
				return mc.G, nil
			}),
		},
	}
	s := NewMatch(g, 0, nil)
	s, _ = Apply(g, s, MoveRequest{PlayerID: "0", Move: "queue"})
	if _, ok := s.Ctx.ActivePlayers["1"]; !ok || len(s.Ctx.ActivePlayers) != 1 {
		t.Fatalf("expected only player 1 active first, got %v", s.Ctx.ActivePlayers)
	}
	s, _ = Apply(g, s, MoveRequest{PlayerID: "1", Move: "act"})
	if _, ok := s.Ctx.ActivePlayers["2"]; !ok || len(s.Ctx.ActivePlayers) != 1 {
		t.Fatalf("expected only player 2 active after 1 finishes, got %v", s.Ctx.ActivePlayers)
	}
	s, _ = Apply(g, s, MoveRequest{PlayerID: "2", Move: "act"})
	if len(s.Ctx.ActivePlayers) != 0 {
		t.Fatalf("expected empty active set after queue drained, got %v", s.Ctx.ActivePlayers)
	}
}
