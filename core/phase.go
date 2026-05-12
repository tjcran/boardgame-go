package core

// PhaseConfig defines a named phase of the game. Phases override the
// surrounding Moves and Turn config for their duration.
//
// Mirrors BGIO's `phases.{name}`.
type PhaseConfig struct {
	// OnBegin runs when this phase becomes active.
	OnBegin HookFn

	// OnEnd runs when this phase is left.
	OnEnd HookFn

	// EndIf, if set, is checked after every move (after Turn.EndIf). end=true
	// ends the phase. If next is non-empty, that phase is entered next
	// (overrides the static Next field for this end-of-phase).
	EndIf func(mc *MoveContext) (end bool, next string)

	// Moves replaces the global move table for the duration of the phase.
	// nil keeps the global table in effect.
	Moves map[string]any

	// Turn replaces the global TurnConfig for the duration of the phase.
	Turn *TurnConfig

	// Start marks this phase as the initial phase of the game. Exactly zero
	// or one phase may set Start: true.
	Start bool

	// Next is the phase to enter when EndIf returns end=true (without a
	// next override). Accepts either a string (static) or
	// `func(*MoveContext) string` (dynamic). Empty / nil means the game
	// leaves all phases (ctx.Phase becomes "").
	Next any
}

// resolveNextPhase evaluates the Next field for a given MoveContext.
// Returns an empty string when no next phase is configured.
func (p *PhaseConfig) resolveNextPhase(mc *MoveContext) string {
	switch n := p.Next.(type) {
	case nil:
		return ""
	case string:
		return n
	case func(*MoveContext) string:
		return n(mc)
	default:
		return ""
	}
}
