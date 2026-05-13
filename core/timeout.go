package core

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
// from OnEnd is also processed).
func ForceEndTurn(game *Game, state State) State {
	if state.Ctx.Gameover != nil {
		return state
	}
	events := &Events{}
	mc := &MoveContext{G: state.G, Ctx: state.Ctx, Events: events}
	state = endTurn(game, state, "", events)
	state, _ = drainEvents(game, state, mc, events)
	return state
}
