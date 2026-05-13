package core

import (
	"errors"
	"fmt"
	"strings"
)

// Validate inspects a Game definition for configuration bugs that would
// otherwise only surface at runtime. Returns nil for a well-formed Game.
// `Manager.Register` calls this on every game; tests can call it
// directly.
//
// Checks performed:
//
//   - Setup is non-nil (an engine without one can't build a match).
//   - At most one phase has Start: true.
//   - Every Phase.Next that's a static string names a phase that exists.
//   - Every move referenced from `setStage`/`setActivePlayers` in a
//     stage's Moves table exists at registration time (we can't know
//     about runtime SetStage args).
//   - Stage names referenced from Phase.Turn.Stages must be unique within
//     their phase (Go maps already enforce this, but we surface a clear
//     error if a tooling layer hands us duplicates).
//   - Move table values normalise into a Move (catches a stray `nil` or
//     a wrongly-typed function).
//
// Returned errors join multiple problems where useful so authors see the
// full list, not just the first.
func (g *Game) Validate() error {
	var errs []string

	if g.Setup == nil {
		errs = append(errs, "Setup is nil")
	}

	// At most one start phase.
	starts := []string{}
	for name, p := range g.Phases {
		if p.Start {
			starts = append(starts, name)
		}
	}
	if len(starts) > 1 {
		errs = append(errs, fmt.Sprintf("multiple phases marked Start=true: %v", starts))
	}

	// Static Phase.Next must point at a real phase.
	for name, p := range g.Phases {
		if s, ok := p.Next.(string); ok && s != "" {
			if _, exists := g.Phases[s]; !exists {
				errs = append(errs,
					fmt.Sprintf("phase %q has Next=%q but that phase doesn't exist", name, s))
			}
		}
	}

	// Global moves table normalises.
	for name, raw := range g.Moves {
		if _, err := asMove(raw); err != nil {
			errs = append(errs, fmt.Sprintf("move %q: %v", name, err))
		}
	}

	// Phase moves and stage moves normalise.
	for phaseName, p := range g.Phases {
		for name, raw := range p.Moves {
			if _, err := asMove(raw); err != nil {
				errs = append(errs,
					fmt.Sprintf("phase %q move %q: %v", phaseName, name, err))
			}
		}
		if p.Turn != nil {
			for stageName, st := range p.Turn.Stages {
				for name, raw := range st.Moves {
					if _, err := asMove(raw); err != nil {
						errs = append(errs,
							fmt.Sprintf("phase %q stage %q move %q: %v",
								phaseName, stageName, name, err))
					}
				}
			}
		}
	}
	if g.Turn != nil {
		for stageName, st := range g.Turn.Stages {
			for name, raw := range st.Moves {
				if _, err := asMove(raw); err != nil {
					errs = append(errs,
						fmt.Sprintf("stage %q move %q: %v", stageName, name, err))
				}
			}
		}
	}

	// Player count sanity.
	if g.MinPlayers > 0 && g.MaxPlayers > 0 && g.MinPlayers > g.MaxPlayers {
		errs = append(errs,
			fmt.Sprintf("MinPlayers (%d) > MaxPlayers (%d)", g.MinPlayers, g.MaxPlayers))
	}

	if len(errs) == 0 {
		return nil
	}
	return errors.New("invalid game: " + strings.Join(errs, "; "))
}
