package starlarkgame

import (
	"context"
	"fmt"
)

// Validate runs registration-time smoke checks. Parse and META checks
// already happened in LoadSpec; this layer is the dynamic part:
//  1. setup(ctx) succeeds for min and max players, returns a dict.
//  2. end_if on the initial state returns None (game isn't over at start).
//  3. legal_moves on the initial state returns a list of {name,args}
//     whose names are all in MOVES.
//
// All calls run under the same step / wall caps as live play.
func Validate(ctx context.Context, s *Spec) error {
	for _, n := range []int{s.Meta.MinPlayers, s.Meta.MaxPlayers} {
		bc := &BridgeCtx{NumPlayers: n}
		bc.AttachSeededRandom(0)
		state, err := s.CallSetup(ctx, bc)
		if err != nil {
			return fmt.Errorf("setup smoke (n=%d): %w", n, err)
		}

		end, err := s.CallEndIf(ctx, bc, state)
		if err != nil {
			return fmt.Errorf("end_if smoke (n=%d): %w", n, err)
		}
		if end != nil {
			return fmt.Errorf("end_if returned non-nil at setup (n=%d): %v", n, end)
		}

		lm, err := s.CallLegalMoves(ctx, bc, state)
		if err != nil {
			return fmt.Errorf("legal_moves smoke (n=%d): %w", n, err)
		}
		for i, mv := range lm {
			name, _ := mv["name"].(string)
			if _, ok := s.Moves[name]; !ok {
				return fmt.Errorf("legal_moves[%d] names %q not in MOVES", i, name)
			}
		}
	}
	return nil
}
