package core

// Undo reverts the most recent undoable move in the current turn, returning
// the engine to the state captured just before that move. Calls outside the
// undoable window (or when undo is disabled) return ErrInvalidMove.
//
// Mirrors boardgame.io's client.undo. The scope is intentionally limited to
// the current turn — once a turn ends, the snapshot stack is cleared so
// undo cannot reach into a prior player's turn.
func Undo(game *Game, state State) (State, error) {
	if game.DisableUndo {
		return state, ErrInvalidMove
	}
	if state.Ctx.Gameover != nil {
		return state, ErrGameOver
	}
	if len(state.TurnSnapshots) == 0 {
		return state, ErrInvalidMove
	}
	last := state.TurnSnapshots[len(state.TurnSnapshots)-1]
	popped := state.Log[len(state.Log)-1]
	last.Undone = append(state.Undone, popped)
	// Truncate the log to drop the undone entry; keep prior history.
	last.Log = state.Log[:len(state.Log)-1]
	// OnUndo intercept: lets the game scrub transient fields that
	// shouldn't replay on undo (animations, sounds, hint highlights).
	if game.OnUndo != nil {
		mc := &MoveContext{G: last.G, Ctx: last.Ctx, Events: &Events{}}
		last.G = game.OnUndo(mc)
	}
	return last, nil
}

// Redo replays the most recently undone move. Returns ErrInvalidMove if the
// redo stack is empty or if the game has moved past the original turn.
func Redo(game *Game, state State) (State, error) {
	if game.DisableUndo {
		return state, ErrInvalidMove
	}
	if state.Ctx.Gameover != nil {
		return state, ErrGameOver
	}
	if len(state.Undone) == 0 {
		return state, ErrInvalidMove
	}
	entry := state.Undone[len(state.Undone)-1]
	state.Undone = state.Undone[:len(state.Undone)-1]
	return Apply(game, state, MoveRequest{
		PlayerID: entry.PlayerID,
		Move:     entry.Move,
		Args:     entry.Args,
	})
}

// cloneStateForSnapshot builds a shallow-clone of State suitable for the
// undo stack. Slices/maps are duplicated so subsequent mutations don't leak
// into the snapshot. G is shared by reference — moves are expected to
// return new G values rather than mutate in place.
func cloneStateForSnapshot(s State) State {
	out := s
	out.Log = append([]LogEntry(nil), s.Log...)
	out.Undone = append([]LogEntry(nil), s.Undone...)
	if s.Plugins != nil {
		out.Plugins = make(map[string]any, len(s.Plugins))
		for k, v := range s.Plugins {
			out.Plugins[k] = v
		}
	}
	if s.Ctx.ActivePlayers != nil {
		out.Ctx.ActivePlayers = copyStrMap(s.Ctx.ActivePlayers)
	}
	out.MoveCounts = copyIntMap(s.MoveCounts)
	out.StageMinMoves = copyIntMap(s.StageMinMoves)
	out.StageMaxMoves = copyIntMap(s.StageMaxMoves)
	// TurnSnapshots intentionally not copied — they only matter at the
	// top of the stack, and undo pops from the live state.
	out.TurnSnapshots = nil
	return out
}
