package core

import "strconv"

// TurnConfig is the per-turn configuration. It can live on Game (global
// default), or be overridden inside a PhaseConfig.
//
// Mirrors boardgame.io's `turn` object.
type TurnConfig struct {
	// Order selects how the engine advances the active player. Defaults to
	// TurnOrderDefault if zero.
	Order TurnOrder

	// OnBegin runs at the start of a turn. May call mc.Events.* to queue
	// further transitions, except those forbidden by BGIO's hook table
	// (e.g. SetStage and EndStage are not allowed in OnBegin).
	OnBegin HookFn

	// OnEnd runs when a turn ends, before the next turn begins.
	OnEnd HookFn

	// OnMove runs after every successful move in this scope.
	OnMove HookFn

	// EndIf, if set, is checked after every move (and the standard event
	// drain). Returning end=true ends the turn. If next is non-empty, the
	// next CurrentPlayer is forced to that player ID.
	EndIf func(mc *MoveContext) (end bool, next string)

	// MinMoves blocks endTurn until at least this many counting moves have
	// been played in the current turn. Zero means no minimum.
	MinMoves int

	// MaxMoves auto-ends the turn after this many counting moves. Zero
	// means no maximum.
	MaxMoves int

	// ActivePlayers, if set, is applied at the start of every turn in this
	// scope (equivalent to calling SetActivePlayers in OnBegin).
	ActivePlayers *ActivePlayersConfig

	// Stages defines named intra-turn sub-states. Players move between
	// stages with events.SetStage / events.EndStage.
	Stages map[string]*StageConfig
}

// TurnOrder describes how PlayOrderPos changes between turns. Most games use
// one of the package-level helpers (TurnOrderDefault, TurnOrderOnce, etc).
// For full control, implement First/Next yourself.
//
// Mirrors BGIO's `turn.order` shape:
//
//	{ first, next, playOrder }
type TurnOrder struct {
	// First returns the initial PlayOrderPos for the (re)starting turn
	// sequence in a phase. Required for non-default orders.
	First func(mc *MoveContext) int

	// Next returns the next PlayOrderPos. Returning nil signals the phase
	// should end (used by TurnOrderOnce). Required for non-default orders.
	Next func(mc *MoveContext) *int

	// PlayOrder, if set, overrides ctx.PlayOrder at phase start.
	PlayOrder func(mc *MoveContext) []string
}

// IsDefault reports whether this is the zero TurnOrder, in which case the
// engine uses round-robin (TurnOrderDefault) semantics.
func (o TurnOrder) IsDefault() bool {
	return o.First == nil && o.Next == nil && o.PlayOrder == nil
}

// TurnOrderDefault is the standard round-robin: starts at 0, increments
// mod NumPlayers, never ends the phase on its own.
var TurnOrderDefault = TurnOrder{
	First: func(mc *MoveContext) int { return 0 },
	Next: func(mc *MoveContext) *int {
		n := (mc.Ctx.PlayOrderPos + 1) % mc.Ctx.NumPlayers
		return &n
	},
}

// TurnOrderReset is like Default, but always restarts at index 0 at the
// beginning of each phase.
var TurnOrderReset = TurnOrder{
	First: func(mc *MoveContext) int { return 0 },
	Next:  TurnOrderDefault.Next,
}

// TurnOrderContinue starts the phase with whichever player ended the
// previous phase. (Default starts at 0; this preserves the cursor.)
var TurnOrderContinue = TurnOrder{
	First: func(mc *MoveContext) int { return mc.Ctx.PlayOrderPos },
	Next:  TurnOrderDefault.Next,
}

// TurnOrderOnce is a single round-robin: after every player has had a turn,
// Next returns nil so the engine ends the phase.
var TurnOrderOnce = TurnOrder{
	First: func(mc *MoveContext) int { return 0 },
	Next: func(mc *MoveContext) *int {
		n := mc.Ctx.PlayOrderPos + 1
		if n >= mc.Ctx.NumPlayers {
			return nil
		}
		return &n
	},
}

// TurnOrderCustom returns a TurnOrder that uses the given explicit play
// order array. Equivalent to BGIO's TurnOrder.CUSTOM(['1','3']).
func TurnOrderCustom(order []string) TurnOrder {
	cp := append([]string(nil), order...)
	return TurnOrder{
		First: func(mc *MoveContext) int { return 0 },
		Next: func(mc *MoveContext) *int {
			n := (mc.Ctx.PlayOrderPos + 1) % len(mc.Ctx.PlayOrder)
			return &n
		},
		PlayOrder: func(mc *MoveContext) []string { return cp },
	}
}

// TurnOrderCustomFrom returns a TurnOrder that reads the play-order array
// from a field of G. The getter is called once per phase start.
//
// Equivalent to BGIO's TurnOrder.CUSTOM_FROM('property').
func TurnOrderCustomFrom(getter func(g G) []string) TurnOrder {
	return TurnOrder{
		First: func(mc *MoveContext) int { return 0 },
		Next: func(mc *MoveContext) *int {
			n := (mc.Ctx.PlayOrderPos + 1) % len(mc.Ctx.PlayOrder)
			return &n
		},
		PlayOrder: func(mc *MoveContext) []string { return getter(mc.G) },
	}
}

// defaultPlayOrder builds the ["0", "1", ..., "n-1"] sequence used as the
// initial PlayOrder when no override is supplied.
func defaultPlayOrder(n int) []string {
	out := make([]string, n)
	for i := 0; i < n; i++ {
		out[i] = strconv.Itoa(i)
	}
	return out
}
