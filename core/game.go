// Package core defines the pure game engine: types and a reducer that, given
// a Game definition and a current State, produces the next State in response
// to a Move. Nothing in this package touches I/O or wall-clock time, which
// keeps games deterministic and trivially testable.
//
// The shape closely mirrors boardgame.io's `Game` object so games translate
// across the two frameworks with mechanical changes.
package core

// G is the user-defined game state. The engine never inspects it; it's an
// opaque payload that the move functions own.
type G = any

// SetupFn builds the initial G for a new match. ctx is fully populated by
// the engine; setupData is whatever was passed through the Lobby's
// `create({setupData})` call (typed game-by-game).
type SetupFn func(ctx Ctx, setupData any) G

// ValidateSetupDataFn pre-validates a setupData payload at match-creation
// time. Returning a non-empty string aborts the create with that message.
type ValidateSetupDataFn func(setupData any, numPlayers int) string

// PlayerViewFn redacts G before it leaves the server for a particular seat.
// playerID is empty for spectators.
type PlayerViewFn func(g G, ctx Ctx, playerID string) G

// EndIfFn is checked after every move. Returning a non-nil value ends the
// game and writes the value to ctx.Gameover.
type EndIfFn func(mc *MoveContext) any

// Game is a declarative definition. The framework consumes one of these and
// runs matches against it.
//
// Mirrors boardgame.io's `Game` object — see PARITY.md for field-by-field
// mapping.
type Game struct {
	// Identification.
	Name string

	// Player count bounds. Enforced by the Lobby. MinPlayers=0 means "no
	// minimum"; MaxPlayers=0 means "no maximum"; if both are zero, the
	// engine defaults to 2-player on Setup.
	MinPlayers int
	MaxPlayers int

	// Setup constructs the initial G.
	Setup SetupFn

	// ValidateSetupData optionally rejects malformed setupData.
	ValidateSetupData ValidateSetupDataFn

	// Moves is the global move table. Values must be either a MoveFn (or a
	// function with that signature) for short-form, or a Move struct for
	// long-form. Per-phase moves override these.
	Moves map[string]any

	// Turn is the global TurnConfig. Per-phase Turn configs override fields
	// individually (the engine merges).
	Turn *TurnConfig

	// Phases is the map of named phases. Exactly zero or one phase may set
	// Start: true.
	Phases map[string]*PhaseConfig

	// EndIf, if set, is checked after every successful move. Returning a
	// non-nil value ends the game.
	EndIf EndIfFn

	// OnEnd is called after EndIf fires. Useful for final scoring/cleanup.
	OnEnd HookFn

	// OnUndo, if set, is called when core.Undo has restored the pre-move
	// state but before it's returned to the caller. Lets the game scrub
	// transient fields (animations, sound triggers, UI hints) that
	// shouldn't replay on undo. Addresses BGIO issue #1135 — BGIO has no
	// such hook today.
	OnUndo HookFn

	// PlayerView, if set, is called before pushing state to a client to
	// redact G per-seat.
	PlayerView PlayerViewFn

	// Seed seeds the Random plugin. Accepts a string or int. If zero the
	// engine generates a per-match seed.
	Seed any

	// DisableUndo turns off undo for every move in this game (parity with
	// BGIO's top-level `disableUndo: true`).
	DisableUndo bool

	// DeltaState, when true, makes the transport send JSON Patch diffs
	// instead of full state on update (BGIO's `deltaState`).
	DeltaState bool

	// AllowDynamicPlayers permits new players to join after the match has
	// started: Manager.Join appends a fresh seat to ctx.PlayOrder and
	// bumps ctx.NumPlayers (within MaxPlayers). Addresses BGIO issues
	// #884 / #1102, which BGIO can't support because its engine assumes
	// PlayOrder is fixed.
	AllowDynamicPlayers bool

	// Plugins are applied in order. See core/plugin.go.
	Plugins []Plugin
}

// PlayerCount returns the configured player count for a fresh match,
// defaulting to 2 when neither MinPlayers nor MaxPlayers gives a usable
// upper bound. requested, when non-zero, overrides the default.
func (g *Game) PlayerCount(requested int) int {
	if requested > 0 {
		return requested
	}
	if g.MaxPlayers > 0 {
		return g.MaxPlayers
	}
	if g.MinPlayers > 0 {
		return g.MinPlayers
	}
	return 2
}

// startPhase returns the phase marked Start: true, or "" if no phase is
// initial.
func (g *Game) startPhase() string {
	for name, p := range g.Phases {
		if p.Start {
			return name
		}
	}
	return ""
}

// scopeMoves returns the active move table for a given phase. A nil
// phase-level Moves means "fall back to global". A phase that explicitly
// sets Moves overrides the global table entirely (parity with BGIO).
func (g *Game) scopeMoves(phase string) map[string]any {
	if phase != "" {
		if p, ok := g.Phases[phase]; ok && p.Moves != nil {
			return p.Moves
		}
	}
	return g.Moves
}

// scopeTurn returns the active TurnConfig for a given phase, merging phase
// override over the global. A nil result means "use defaults".
func (g *Game) scopeTurn(phase string) *TurnConfig {
	if phase != "" {
		if p, ok := g.Phases[phase]; ok && p.Turn != nil {
			return p.Turn
		}
	}
	return g.Turn
}
