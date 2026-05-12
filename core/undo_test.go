package core

import (
	"errors"
	"testing"
)

type counterState struct{ Count int }

// multiMoveGame: 1 player, multi-move turn, increments a counter. Lets us
// stack moves in a single turn for undo coverage.
func multiMoveGame(disableUndo bool, undoableInc bool) *Game {
	incMove := Move{
		Move: func(mc *MoveContext, _ ...any) (G, error) {
			return &counterState{Count: mc.G.(*counterState).Count + 1}, nil
		},
		Undoable: undoableInc,
	}
	return &Game{
		Name:        "counter-undo",
		MinPlayers:  1,
		MaxPlayers:  1,
		DisableUndo: disableUndo,
		Setup:       func(_ Ctx, _ any) G { return &counterState{} },
		Moves:       map[string]any{"inc": incMove},
		Turn:        &TurnConfig{}, // multi-move turn; no auto-end.
	}
}

func TestUndoRevertsLastMove(t *testing.T) {
	g := multiMoveGame(false, true)
	s := NewMatch(g, 0, nil)
	for i := 0; i < 3; i++ {
		next, err := Apply(g, s, MoveRequest{PlayerID: "0", Move: "inc"})
		if err != nil {
			t.Fatalf("inc %d: %v", i, err)
		}
		s = next
	}
	if s.G.(*counterState).Count != 3 {
		t.Fatalf("expected count 3, got %d", s.G.(*counterState).Count)
	}
	u, err := Undo(g, s)
	if err != nil {
		t.Fatalf("undo: %v", err)
	}
	if u.G.(*counterState).Count != 2 {
		t.Fatalf("expected count 2 after undo, got %d", u.G.(*counterState).Count)
	}
}

func TestRedoReappliesUndoneMove(t *testing.T) {
	g := multiMoveGame(false, true)
	s := NewMatch(g, 0, nil)
	s, _ = Apply(g, s, MoveRequest{PlayerID: "0", Move: "inc"})
	s, _ = Apply(g, s, MoveRequest{PlayerID: "0", Move: "inc"})
	s, _ = Undo(g, s)
	r, err := Redo(g, s)
	if err != nil {
		t.Fatalf("redo: %v", err)
	}
	if r.G.(*counterState).Count != 2 {
		t.Fatalf("expected count 2 after redo, got %d", r.G.(*counterState).Count)
	}
}

func TestUndoRejectedWhenGameDisabled(t *testing.T) {
	g := multiMoveGame(true, true)
	s := NewMatch(g, 0, nil)
	s, _ = Apply(g, s, MoveRequest{PlayerID: "0", Move: "inc"})
	_, err := Undo(g, s)
	if !errors.Is(err, ErrInvalidMove) {
		t.Fatalf("expected ErrInvalidMove when DisableUndo, got %v", err)
	}
}

func TestUndoRejectedForNonUndoableMove(t *testing.T) {
	g := multiMoveGame(false, false)
	s := NewMatch(g, 0, nil)
	s, _ = Apply(g, s, MoveRequest{PlayerID: "0", Move: "inc"})
	_, err := Undo(g, s)
	if !errors.Is(err, ErrInvalidMove) {
		t.Fatalf("expected ErrInvalidMove for non-undoable move, got %v", err)
	}
}

func TestUndoScopedToCurrentTurn(t *testing.T) {
	g := multiMoveGame(false, true)
	// Force turn end on each inc so we can verify undo doesn't reach
	// across turns. We do this by adding an EndTurn event to the move.
	g.Moves["inc"] = Move{
		Move: func(mc *MoveContext, _ ...any) (G, error) {
			mc.Events.EndTurn()
			return &counterState{Count: mc.G.(*counterState).Count + 1}, nil
		},
		Undoable: true,
	}
	g.MinPlayers, g.MaxPlayers = 2, 2
	g.Turn = &TurnConfig{}
	s := NewMatch(g, 0, nil)
	s, _ = Apply(g, s, MoveRequest{PlayerID: "0", Move: "inc"}) // turn ends → snapshot cleared
	_, err := Undo(g, s)
	if !errors.Is(err, ErrInvalidMove) {
		t.Fatalf("expected ErrInvalidMove after turn-end clears snapshots, got %v", err)
	}
}

func TestRedactedLogHidesArgsFromOthers(t *testing.T) {
	g := &Game{
		Name:       "secrecy",
		MinPlayers: 2,
		MaxPlayers: 2,
		Setup:      func(_ Ctx, _ any) G { return struct{}{} },
		Moves: map[string]any{
			"whisper": Move{
				Move:   func(mc *MoveContext, _ ...any) (G, error) { return mc.G, nil },
				Redact: true,
			},
		},
		Turn: &TurnConfig{MinMoves: 1, MaxMoves: 1},
	}
	s := NewMatch(g, 0, nil)
	s, err := Apply(g, s, MoveRequest{PlayerID: "0", Move: "whisper", Args: []any{"hi"}})
	if err != nil {
		t.Fatalf("whisper: %v", err)
	}
	logForOthers := redactedLog(s.Log, "1")
	if logForOthers[0].Args != nil {
		t.Fatalf("expected args hidden for other player, got %v", logForOthers[0].Args)
	}
	logForSelf := redactedLog(s.Log, "0")
	if len(logForSelf[0].Args) == 0 {
		t.Fatalf("expected args visible to originator, got %v", logForSelf[0].Args)
	}
}
