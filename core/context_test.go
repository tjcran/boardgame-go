package core

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestApplyContextThreadsCtxIntoMove(t *testing.T) {
	var seenDeadline bool
	g := &Game{
		Name:       "ctx",
		MinPlayers: 1,
		MaxPlayers: 1,
		Setup:      func(_ Ctx, _ any) G { return struct{}{} },
		Moves: map[string]any{
			"check": MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
				_, seenDeadline = mc.Context.Deadline()
				return mc.G, nil
			}),
		},
		Turn: &TurnConfig{MinMoves: 1, MaxMoves: 1},
	}
	s := NewMatch(g, 0, nil)

	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()
	if _, err := ApplyContext(ctx, g, s, MoveRequest{PlayerID: "0", Move: "check"}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !seenDeadline {
		t.Fatal("expected the move to observe a deadline from the supplied context")
	}
}

func TestApplyDefaultsToBackgroundContext(t *testing.T) {
	g := &Game{
		Name:       "ctx",
		MinPlayers: 1,
		MaxPlayers: 1,
		Setup:      func(_ Ctx, _ any) G { return struct{}{} },
		Moves: map[string]any{
			"check": MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
				if mc.Context == nil {
					return mc.G, errors.New("nil context")
				}
				return mc.G, nil
			}),
		},
		Turn: &TurnConfig{MinMoves: 1, MaxMoves: 1},
	}
	s := NewMatch(g, 0, nil)
	if _, err := Apply(g, s, MoveRequest{PlayerID: "0", Move: "check"}); err != nil {
		t.Fatalf("expected non-nil ctx with Apply: %v", err)
	}
}
