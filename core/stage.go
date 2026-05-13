package core

// StageNull is the sentinel stage name meaning "this player is active but
// not restricted to a particular stage's move table". Mirrors BGIO's
// Stage.NULL, which is the empty string.
const StageNull = ""

// Stage returns a *string pointer to the given stage name. It's an
// ergonomic helper for the optional fields in ActivePlayersConfig where the
// empty string is a meaningful value (it equals StageNull) and we need to
// distinguish "field not set" from "field set to empty string".
func Stage(name string) *string {
	s := name
	return &s
}

// StageConfig defines an intra-turn sub-state. Stages live inside a
// TurnConfig.Stages map.
//
// Mirrors BGIO's `turn.stages.{name}` — plus OnBegin/OnEnd hooks the BGIO
// docs say should exist but never landed (issue #608).
type StageConfig struct {
	// Moves overrides the active move table for players in this stage. If
	// nil, players in this stage can use the surrounding phase/global moves.
	Moves map[string]any

	// Next, when set, is the stage name a player is transferred to when
	// events.EndStage() is called from this stage. Empty string means the
	// player leaves the active set entirely.
	Next string

	// OnBegin fires when a player enters this stage (via setStage, or via
	// EndStage from a stage whose Next points here). The hook's MoveContext
	// has PlayerID set to the entering player.
	OnBegin HookFn

	// OnEnd fires when a player exits this stage (via endStage, including
	// when a Next chain moves them on). The hook's MoveContext has
	// PlayerID set to the exiting player.
	OnEnd HookFn
}

// ActivePlayersConfig describes how to populate ctx.ActivePlayers, either at
// turn start (via TurnConfig.ActivePlayers) or ad-hoc via
// events.SetActivePlayers(cfg).
//
// The *string fields (CurrentPlayer/Others/All) are pointers because BGIO's
// Stage.NULL is the empty string AND BGIO distinguishes "field not present"
// from "field set to Stage.NULL". Use core.Stage("name") to set, and leave
// nil to mean "don't add these players".
//
// Mirrors BGIO's `setActivePlayers` argument.
type ActivePlayersConfig struct {
	// CurrentPlayer assigns the current player a stage. Nil leaves them
	// out of the active set.
	CurrentPlayer *string

	// Others assigns every non-current player a stage. Nil leaves them
	// out of the active set.
	Others *string

	// All assigns every player a stage. Nil leaves them out of the
	// active set.
	All *string

	// Value enumerates explicit player → stage mappings. Overrides
	// CurrentPlayer/Others/All for listed players (last-write-wins).
	Value map[string]string

	// MinMoves applies to every entered active player.
	MinMoves int

	// MaxMoves applies to every entered active player.
	MaxMoves int

	// PerPlayerMinMoves overrides MinMoves for specific players.
	PerPlayerMinMoves map[string]int

	// PerPlayerMaxMoves overrides MaxMoves for specific players.
	PerPlayerMaxMoves map[string]int

	// Revert restores the previous active-player set after this one
	// drains to empty.
	Revert bool

	// Next is applied after this active-player set drains (alternative to
	// Revert). When both are set, Revert wins.
	Next *ActivePlayersConfig
}

// ActivePlayersAll is the preset matching BGIO's `ActivePlayers.ALL`:
// every player is active, none restricted to a stage.
var ActivePlayersAll = ActivePlayersConfig{All: Stage(StageNull)}

// ActivePlayersAllOnce matches BGIO's `ActivePlayers.ALL_ONCE`: every player
// is active and may play exactly one move before being removed.
var ActivePlayersAllOnce = ActivePlayersConfig{All: Stage(StageNull), MinMoves: 1, MaxMoves: 1}

// ActivePlayersOthers matches BGIO's `ActivePlayers.OTHERS`: every player
// except the current player is active.
var ActivePlayersOthers = ActivePlayersConfig{Others: Stage(StageNull)}

// ActivePlayersOthersOnce matches BGIO's `ActivePlayers.OTHERS_ONCE`: every
// player except the current player is active and may play exactly one move.
var ActivePlayersOthersOnce = ActivePlayersConfig{Others: Stage(StageNull), MinMoves: 1, MaxMoves: 1}
