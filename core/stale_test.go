package core

import (
	"errors"
	"testing"
)

func TestStaleStateRejected(t *testing.T) {
	g := &Game{
		Name:       "stale",
		MinPlayers: 2,
		MaxPlayers: 2,
		Setup:      func(_ Ctx, _ any) G { return struct{}{} },
		Moves:      map[string]any{"x": noopMove},
		Turn:       &TurnConfig{MinMoves: 1, MaxMoves: 1},
	}
	s := NewMatch(g, 0, nil)
	// First move advances StateID to 1.
	s, _ = Apply(g, s, MoveRequest{PlayerID: "0", Move: "x"})
	// Now player 1 sends a move actively claiming to be on the old state.
	_, err := Apply(g, s, MoveRequest{PlayerID: "1", Move: "x", StateID: 999})
	if !errors.Is(err, ErrStaleState) {
		t.Fatalf("expected ErrStaleState, got %v", err)
	}
	// Sending with the current StateID succeeds.
	if _, err := Apply(g, s, MoveRequest{PlayerID: "1", Move: "x", StateID: s.StateID}); err != nil {
		t.Fatalf("expected success with current StateID, got %v", err)
	}
}

func TestIgnoreStaleStateIDAllowsStaleClient(t *testing.T) {
	g := &Game{
		Name:       "ignore-stale",
		MinPlayers: 2,
		MaxPlayers: 2,
		Setup:      func(_ Ctx, _ any) G { return struct{}{} },
		Moves: map[string]any{
			"x": Move{
				Move:               func(mc *MoveContext, _ ...any) (G, error) { return mc.G, nil },
				IgnoreStaleStateID: true,
			},
		},
		Turn: &TurnConfig{MinMoves: 1, MaxMoves: 1},
	}
	s := NewMatch(g, 0, nil)
	s, _ = Apply(g, s, MoveRequest{PlayerID: "0", Move: "x"})
	if _, err := Apply(g, s, MoveRequest{PlayerID: "1", Move: "x", StateID: 999}); err != nil {
		t.Fatalf("expected stale move to succeed when IgnoreStaleStateID=true, got %v", err)
	}
}
