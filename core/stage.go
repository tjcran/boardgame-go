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
	// Moves is the move table for players in this stage. It takes
	// precedence over the surrounding phase/global table: a name defined
	// here resolves to this stage's move. By default it does not replace
	// that table — a name missing here (or every name, when Moves is nil)
	// still resolves from the phase/global moves. Set Exclusive to make
	// this table the complete set of moves a player in the stage may make.
	//
	// When the active phase's Turn.Stages and the game-level Turn.Stages
	// both define a stage name, the phase's entry is that stage while the
	// phase is active: its Moves, Exclusive flag and hooks apply, and the
	// game-level entry's Moves are not consulted.
	Moves map[string]any

	// Exclusive, when true, restricts a player in this stage to the moves
	// listed in Moves; any other move is rejected with ErrMoveNotInStage
	// instead of falling through to the phase/global table. This matches
	// boardgame.io, where a stage that defines moves is exclusive.
	//
	// Some moves stay legal regardless, because refusing them would
	// strand the match or override the server:
	//
	//   - Moves flagged AnyPlayer or IgnoreBlocks (concede / forfeit /
	//     timeout / emergency exit).
	//   - The answer to a block addressed to the caller, when the block
	//     names its answer move (AnsweredBy) and the request is that
	//     move. The engine asked that player a question; their answer
	//     must be accepted wherever its move is registered. A block that
	//     names no answer move gets no exemption, since any move could
	//     carry its tag: register its answer in this table instead.
	//   - Moves the server dispatches on its own authority
	//     (MoveRequest.ServerDispatch, set by match.Manager.DispatchServer).
	//     Drain steps and Events.RunMove are not stage-scoped either.
	//
	// An Exclusive stage with nil Moves therefore permits only those
	// exempt moves. That is a deliberate way to hold a player until they
	// answer a prompt that names its answer, or until the server or an
	// AnyPlayer move releases them, so Game.Validate accepts it: whether
	// such a release exists is only known at runtime. A game that gates
	// a player into such a stage must raise that prompt or provide that
	// release, or the player is left with no legal move.
	//
	// Opt-in because the default fall-through is load-bearing for
	// existing games (for example a resume or concede move registered
	// only at phase scope). Turning it on for a game that already has
	// persisted matches can make Replay of their logs fail: a recorded
	// move that fell through to the phase/global table when it was
	// played is now rejected with ErrMoveNotInStage.
	Exclusive bool

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

// ActivePlayersInOrder builds an ActivePlayersConfig chain that activates
// each listed player one at a time, in order. The next player becomes
// active when the current one drains their move budget (maxMoves) or
// calls events.EndStage. After the last player, the chain ends and
// ctx.ActivePlayers returns to nil.
//
// Addresses BGIO issue #478 — "setActivePlayers mode that allows cycling
// through players in order." BGIO's setActivePlayers can activate all
// players at once or a subset, but not sequentially.
//
// stage is the stage name every activated player enters (use StageNull
// for "active, no stage restriction"). maxMoves is per player — pass 0
// to allow unbounded moves until each player calls EndStage manually.
func ActivePlayersInOrder(players []string, stage string, minMoves, maxMoves int) ActivePlayersConfig {
	if len(players) == 0 {
		return ActivePlayersConfig{}
	}
	// Build the chain from the end backwards so each config's Next
	// points at the head of the remaining queue.
	cfg := ActivePlayersConfig{
		Value:    map[string]string{players[len(players)-1]: stage},
		MinMoves: minMoves,
		MaxMoves: maxMoves,
	}
	for i := len(players) - 2; i >= 0; i-- {
		prev := cfg
		cfg = ActivePlayersConfig{
			Value:    map[string]string{players[i]: stage},
			MinMoves: minMoves,
			MaxMoves: maxMoves,
			Next:     &prev,
		}
	}
	return cfg
}
