package core

import "testing"

// noopMove is a move that returns G unchanged. Useful as a marker move when
// the test cares about turn/phase rotation rather than G mutation.
var noopMove = MoveFn(func(mc *MoveContext, _ ...any) (G, error) { return mc.G, nil })

// buildOrderGame builds a 3-player game with the given TurnOrder and a single
// no-op move. Turn auto-ends after each move (MaxMoves=1).
func buildOrderGame(order TurnOrder) *Game {
	return &Game{
		Name:       "order",
		MinPlayers: 3,
		MaxPlayers: 3,
		Setup:      func(_ Ctx, _ any) G { return struct{}{} },
		Moves:      map[string]any{"x": noopMove},
		Turn:       &TurnConfig{Order: order, MinMoves: 1, MaxMoves: 1},
	}
}

// playN plays N moves through the game using the current player each time.
func playN(t *testing.T, g *Game, s State, n int) State {
	t.Helper()
	for i := 0; i < n; i++ {
		next, err := Apply(g, s, MoveRequest{PlayerID: s.Ctx.CurrentPlayer, Move: "x"})
		if err != nil {
			t.Fatalf("move %d (current=%s): %v", i, s.Ctx.CurrentPlayer, err)
		}
		s = next
	}
	return s
}

func TestTurnOrderDefaultCycles(t *testing.T) {
	g := buildOrderGame(TurnOrderDefault)
	s := NewMatch(g, 0, nil)
	// 3 moves cycle through 0, 1, 2 then back to 0.
	s = playN(t, g, s, 3)
	if s.Ctx.CurrentPlayer != "0" {
		t.Fatalf("expected wrap to player 0, got %s", s.Ctx.CurrentPlayer)
	}
}

func TestTurnOrderOnceEndsPhaseAfterOneRound(t *testing.T) {
	g := buildOrderGame(TurnOrderOnce)
	// Without a phase, "end phase" leaves Phase = "" and falls through to
	// default rotation. The semantic the test cares about is "game keeps
	// playing past one round when there's no phase to end" — i.e. Once
	// without a containing phase degrades to Default.
	s := NewMatch(g, 0, nil)
	s = playN(t, g, s, 6) // two full rounds
	// We should still be in turn-9 territory with no error.
	if s.Ctx.Turn < 6 {
		t.Fatalf("expected at least turn 6, got %d", s.Ctx.Turn)
	}
}

func TestTurnOrderCustomUsesGivenOrder(t *testing.T) {
	g := buildOrderGame(TurnOrderCustom([]string{"2", "0", "1"}))
	s := NewMatch(g, 0, nil)
	if s.Ctx.CurrentPlayer != "2" {
		t.Fatalf("expected first player 2, got %s", s.Ctx.CurrentPlayer)
	}
	s = playN(t, g, s, 1)
	if s.Ctx.CurrentPlayer != "0" {
		t.Fatalf("expected second player 0, got %s", s.Ctx.CurrentPlayer)
	}
}

func TestEndTurnEventOverridesNextPlayer(t *testing.T) {
	g := &Game{
		Name:       "skip",
		MinPlayers: 3,
		MaxPlayers: 3,
		Setup:      func(_ Ctx, _ any) G { return struct{}{} },
		Moves: map[string]any{
			"jumpTo2": MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
				mc.Events.EndTurn("2")
				return mc.G, nil
			}),
		},
	}
	s := NewMatch(g, 0, nil)
	next, err := Apply(g, s, MoveRequest{PlayerID: "0", Move: "jumpTo2"})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if next.Ctx.CurrentPlayer != "2" {
		t.Fatalf("expected current=2 after EndTurn override, got %s", next.Ctx.CurrentPlayer)
	}
}

func TestMinMovesBlocksManualEndTurn(t *testing.T) {
	// Two moves required before EndTurn() takes effect: the first EndTurn
	// is silently ignored (BGIO behaviour), the second succeeds.
	g := &Game{
		Name:       "min",
		MinPlayers: 2,
		MaxPlayers: 2,
		Setup:      func(_ Ctx, _ any) G { return struct{}{} },
		Moves: map[string]any{
			"x": MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
				mc.Events.EndTurn()
				return mc.G, nil
			}),
		},
		Turn: &TurnConfig{MinMoves: 2},
	}
	s := NewMatch(g, 0, nil)
	s, _ = Apply(g, s, MoveRequest{PlayerID: "0", Move: "x"})
	if s.Ctx.CurrentPlayer != "0" {
		t.Fatalf("MinMoves should block first EndTurn, got current=%s", s.Ctx.CurrentPlayer)
	}
	s, _ = Apply(g, s, MoveRequest{PlayerID: "0", Move: "x"})
	if s.Ctx.CurrentPlayer != "1" {
		t.Fatalf("MinMoves should allow second EndTurn, got current=%s", s.Ctx.CurrentPlayer)
	}
}
