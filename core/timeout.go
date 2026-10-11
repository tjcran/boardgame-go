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
// ForceEndTurn is atomic like Apply. If a pushed move fails, or the drain
// exceeds MaxDrainDepth, it returns the state it was given, unchanged:
// the turn does not end, just as Apply refuses a move that ends the turn
// into the same failure. A timer that fires again later gets the same
// outcome until the game state changes. (As with Apply, G and plugin data
// that game code mutates in place, rather than returning new values, are
// not rolled back.)
func ForceEndTurn(game *Game, state State) State {
	if state.Ctx.Gameover != nil {
		return state
	}
	// Turn.OnEnd fires from here just as it does from events.EndTurn, so it
	// gets the same plugin surface and queues — and the same post-hook
	// flush. Work on private containers so a failed cascade can hand back
	// the caller's state untouched.
	env := newHookEnv(game, state)
	next := ownContainers(state)
	mc := env.mc(next, "")
	next = endTurn(game, next, "", env)
	next, _ = drainEvents(game, next, mc, env)
	mc.G, mc.Ctx = next.G, next.Ctx
	// Flush before the cascade: each drain step builds its plugin APIs
	// from State.Plugins.
	next = flushPlugins(game, next, mc)
	next, err := drainCascade(context.Background(), game, next, env.queue, -1)
	if err != nil {
		return state
	}
	return next
}
