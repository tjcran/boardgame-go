package core

import "context"

// ForceEndTurn runs the same end-of-turn pipeline `events.EndTurn` would
// from inside a move — Turn.OnEnd hook, advance via the configured
// TurnOrder, Turn.OnBegin for the new turn — except the caller drives it
// from outside the move path (typically a turn-timer fire).
//
// Unlike events.EndTurn this isn't gated by Turn.MinMoves: the engine
// trusts the caller's authority. If a game wants the MinMoves floor to
// apply to timer fires too, the timer scheduler can refuse to call this
// when NumMoves < MinMoves.
//
// Returns the state after one full drain (so a chained endPhase / endGame
// from OnEnd is also processed). The hooks get the same MoveContext a
// move's hooks get, Queue included: a block they raise (a start-of-turn
// prompt from the next turn's OnBegin) is in the returned State.Blocks,
// and moves they push drain before ForceEndTurn returns, exactly as Apply
// drains a move's cascade, stopping at the first block or at game over.
// Their log entries carry Parent -1, since no move started them.
//
// The ending player's pending blocks expire with their turn. A player
// timed out mid-prompt would otherwise keep gating every move of the
// next player, who could not act until the absent player answered.
// ForceEndTurn drops every block in State.Blocks addressed to the
// CurrentPlayer it was called with, as a move's DropBlocksFor would, and
// it honours DropBlocksFor calls from the hooks it runs. Blocks raised
// during the forced turn end, including any for the ending player,
// merge after the drop and survive it. Moves the dropped blocks were
// holding back on State.Queue then drain with the rest, as they do
// after a move's DropBlocksFor.
//
// A successful forced turn end is a state change like an accepted move,
// so it bumps State.StateID once: a client still holding the previous
// StateID is refused with ErrStaleState until it has seen the new turn.
//
// ForceEndTurn is atomic like Apply. If a pushed move fails, or the drain
// exceeds MaxDrainDepth, it returns the state it was given, unchanged:
// the turn does not end, just as Apply refuses a move that ends the turn
// into the same failure. Use ForceEndTurnE to learn that it failed and
// why. (As with Apply, G and plugin data that game code mutates in place,
// rather than returning new values, are not rolled back.)
func ForceEndTurn(game *Game, state State) State {
	next, _ := ForceEndTurnE(game, state)
	return next
}

// ForceEndTurnE is ForceEndTurn that reports failure. On error it returns
// the state it was given, unchanged, with the error: ErrGameOver when the
// game is already over, else the failing drain step's error or
// ErrDrainOverflow.
func ForceEndTurnE(game *Game, state State) (State, error) {
	if state.Ctx.Gameover != nil {
		return state, ErrGameOver
	}
	ending := state.Ctx.CurrentPlayer
	// Turn.OnEnd fires from here just as it does from events.EndTurn, so it
	// gets the same plugin surface and queues — and the same post-hook
	// flush. Work on private containers so a failed cascade can hand back
	// the caller's state untouched.
	env := newHookEnv(game, state)
	next := ownContainers(state)
	mc := env.mc(next, "")
	next = endTurn(game, next, "", env)
	next, err := drainEvents(game, next, mc, env)
	if err != nil {
		return state, err
	}
	mc.G, mc.Ctx = next.G, next.Ctx
	// Flush before the cascade: each drain step builds its plugin APIs
	// from State.Plugins.
	next = flushPlugins(game, next, mc)
	// Expire the ending player's prompts (and any a hook dropped) before
	// the hooks' new blocks merge, so those survive.
	next.Blocks = withoutBlocksFor(next.Blocks, append([]string{ending}, *env.drops...))
	next, err = drainCascade(context.Background(), game, next, env.queue, -1)
	if err != nil {
		return state, err
	}
	next.StateID = state.StateID + 1
	return next, nil
}
