package core

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMoveTimeoutCancelsContext(t *testing.T) {
	var observedErr error
	g := &Game{
		Name:       "timeout",
		MinPlayers: 1,
		MaxPlayers: 1,
		Setup:      func(_ Ctx, _ any) G { return struct{}{} },
		Moves: map[string]any{
			"slow": Move{
				Move: func(mc *MoveContext, _ ...any) (G, error) {
					select {
					case <-mc.Context.Done():
						observedErr = mc.Context.Err()
						return mc.G, mc.Context.Err()
					case <-time.After(200 * time.Millisecond):
						return mc.G, nil
					}
				},
				Timeout: 20 * time.Millisecond,
			},
		},
	}
	s := NewMatch(g, 0, nil)
	_, err := ApplyContext(context.Background(), g, s, MoveRequest{
		PlayerID: "0", Move: "slow",
	})
	if err == nil {
		t.Fatalf("expected move to surface deadline error")
	}
	if !errors.Is(observedErr, context.DeadlineExceeded) {
		t.Fatalf("expected DeadlineExceeded in move ctx, got %v", observedErr)
	}
}

func TestMoveWithoutTimeoutSeesParentContext(t *testing.T) {
	g := &Game{
		Name:       "no-timeout",
		MinPlayers: 1,
		MaxPlayers: 1,
		Setup:      func(_ Ctx, _ any) G { return struct{}{} },
		Moves: map[string]any{
			"ok": MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
				if _, hasDeadline := mc.Context.Deadline(); hasDeadline {
					t.Error("Move with no Timeout should not get a deadline")
				}
				return mc.G, nil
			}),
		},
		Turn: &TurnConfig{MinMoves: 1, MaxMoves: 1},
	}
	s := NewMatch(g, 0, nil)
	if _, err := Apply(g, s, MoveRequest{PlayerID: "0", Move: "ok"}); err != nil {
		t.Fatalf("apply: %v", err)
	}
}
