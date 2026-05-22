package starlarkgame

import (
	"context"
	"fmt"
	"regexp"
)

var moduleRefRe = regexp.MustCompile(`ctx\.modules\.([a-z_]+)`)

// checkModuleRefs statically rejects any ctx.modules.<name> reference whose
// <name> is not declared in the top-level MODULES list. This surfaces a clear
// error at validate time instead of a confusing runtime "no .modules
// attribute" failure during the setup smoke check.
func checkModuleRefs(s *Spec) error {
	declared := map[string]bool{}
	for _, m := range s.Modules {
		declared[m] = true
	}
	for _, mm := range moduleRefRe.FindAllStringSubmatch(s.Source(), -1) {
		if !declared[mm[1]] {
			return fmt.Errorf("spec uses ctx.modules.%s but %q is not in MODULES", mm[1], mm[1])
		}
	}
	return nil
}

// Validate runs registration-time smoke checks. Parse and META checks
// already happened in LoadSpec; this layer is the dynamic part:
//  1. setup(ctx) succeeds for min and max players, returns a dict.
//  2. end_if on the initial state returns None (game isn't over at start).
//  3. legal_moves on the initial state returns a list of {name,args}
//     whose names are all in MOVES.
//
// All calls run under the same step / wall caps as live play.
func Validate(ctx context.Context, s *Spec) error {
	if err := checkModuleRefs(s); err != nil {
		return err
	}
	for _, n := range []int{s.Meta.MinPlayers, s.Meta.MaxPlayers} {
		// Instantiate declared modules so setup smoke can call
		// ctx.modules.<name>.<op>(...), mirroring live Setup.
		mods := s.NewModuleStates()
		bc := &BridgeCtx{NumPlayers: n, Modules: mods}
		bc.AttachSeededRandom(0)
		state, err := s.CallSetup(ctx, bc)
		if err != nil {
			return fmt.Errorf("setup smoke (n=%d): %w", n, err)
		}

		// Read-side smoke (end_if, legal_moves) runs under the same
		// read-only module contract as live play: same module states, but
		// mutating ops error. PlayerID "0" is the canonical seat-0 viewpoint
		// — multi-action specs (Catan-shape) crash on an empty player_id.
		smokeBC := &BridgeCtx{NumPlayers: n, PlayerID: "0", Modules: mods, ReadOnly: true}
		smokeBC.AttachSeededRandom(0)

		end, err := s.CallEndIf(ctx, smokeBC, state)
		if err != nil {
			return fmt.Errorf("end_if smoke (n=%d): %w", n, err)
		}
		if end != nil {
			return fmt.Errorf("end_if returned non-nil at setup (n=%d): %v", n, end)
		}

		lm, err := s.CallLegalMoves(ctx, smokeBC, state)
		if err != nil {
			return fmt.Errorf("legal_moves smoke (n=%d): %w", n, err)
		}
		for i, mv := range lm {
			name := legalMoveName(mv)
			if _, ok := s.Moves[name]; !ok {
				return fmt.Errorf("legal_moves[%d] names %q not in MOVES", i, name)
			}
		}
	}
	return nil
}
