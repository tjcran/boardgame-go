package core

// CanUndo reports whether Undo would succeed against the given state.
// Useful for clients that want to grey out an "Undo" button without
// actually invoking it. Mirrors the API ask in BGIO issue #1048.
func CanUndo(game *Game, state State) bool {
	if game.DisableUndo {
		return false
	}
	if state.Ctx.Gameover != nil {
		return false
	}
	return len(state.TurnSnapshots) > 0
}

// CanRedo is the symmetric helper for Redo.
func CanRedo(game *Game, state State) bool {
	if game.DisableUndo {
		return false
	}
	if state.Ctx.Gameover != nil {
		return false
	}
	return len(state.Undone) > 0
}

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
	// Clip the capacity too: the dropped slot is still part of the
	// input's (and any Clone's) log, and without the clip the next
	// append would overwrite it in place.
	last.Log = clip(state.Log[:len(state.Log)-1])
	// OnUndo intercept: lets the game scrub transient fields that
	// shouldn't replay on undo (animations, sounds, hint highlights).
	if game.OnUndo != nil {
		// Deliberately no plugin table (so no mc.Random): OnUndo runs on a
		// rollback path with nothing to flush into, and letting an undo
		// consume PRNG state would advance a stream the redo can no longer
		// reproduce. Scrubbing transient fields needs no entropy.
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
		PlayerID:       entry.PlayerID,
		Move:           entry.Move,
		Args:           entry.Args,
		ServerDispatch: entry.ServerDispatch,
	})
}

// ownContainers returns s with private copies of the containers the
// reducer writes into in place: the stage bookkeeping maps, Plugins, and
// the Revert stack, whose frames' maps become the live maps again when
// popped (and a popped slot is reused by the next push). Changing the
// result can therefore never change s. Nil stays nil and empty stays
// empty, since a non-nil empty ActivePlayers means something different
// from a nil one.
//
// G and plugin data values are shared: moves and plugins return new
// values rather than mutate them. Slices the reducer only appends to
// (Log, Queue, TurnSnapshots) or rebuilds fresh when it removes an
// element (Blocks, Ctx.PlayOrder) are shared too, which keeps a move in
// a game without stages or plugins free of copies.
func ownContainers(s State) State {
	if s.Plugins != nil {
		plugins := make(map[string]any, len(s.Plugins))
		for k, v := range s.Plugins {
			plugins[k] = v
		}
		s.Plugins = plugins
	}
	s.Ctx.ActivePlayers = copyStrMap(s.Ctx.ActivePlayers)
	s.MoveCounts = copyIntMap(s.MoveCounts)
	s.StageMinMoves = copyIntMap(s.StageMinMoves)
	s.StageMaxMoves = copyIntMap(s.StageMaxMoves)
	if s.ActiveStack != nil {
		stack := make([]activeFrame, len(s.ActiveStack))
		for i, f := range s.ActiveStack {
			stack[i] = activeFrame{
				ActivePlayers: copyStrMap(f.ActivePlayers),
				MoveCounts:    copyIntMap(f.MoveCounts),
				StageMin:      copyIntMap(f.StageMin),
				StageMax:      copyIntMap(f.StageMax),
			}
		}
		s.ActiveStack = stack
	}
	return s
}

// Clone returns a copy of s that a caller can keep and read while the
// original goes on being advanced. The containers the reducer writes in
// place (see ownContainers) are copied; the slices it only appends to
// (Log, Undone, TurnSnapshots, Queue, Blocks, Ctx.PlayOrder) are cut to
// their length, so an append on either side can never write into the
// other's view. Where the engine shortens one of those slices (Undo
// dropping the last log entry) it also drops the spare capacity, so a
// later append reallocates instead of overwriting an element an older
// clone still holds. (Redo's shortened Undone never reaches a result:
// the move it replays clears Undone.) G and plugin data are shared by
// reference, as everywhere else: moves return new values rather than
// mutate them.
func (s State) Clone() State {
	out := ownContainers(s)
	out.rawG, out.rawPlugins = nil, nil
	out.Log = clip(s.Log)
	out.Undone = clip(s.Undone)
	out.TurnSnapshots = clip(s.TurnSnapshots)
	out.Queue = clip(s.Queue)
	out.Blocks = clip(s.Blocks)
	out.Ctx.PlayOrder = clip(s.Ctx.PlayOrder)
	return out
}

// clip drops a slice's spare capacity; nil stays nil.
func clip[T any](s []T) []T { return s[:len(s):len(s)] }

// cloneStateForSnapshot builds a shallow-clone of State suitable for the
// undo stack. Its containers are its own (ownContainers), so the live
// state's later in-place writes don't leak into the snapshot, and the
// log is copied too. G is shared by reference — moves are expected to
// return new G values rather than mutate in place.
func cloneStateForSnapshot(s State) State {
	out := ownContainers(s)
	out.Log = append([]LogEntry(nil), s.Log...)
	out.Undone = append([]LogEntry(nil), s.Undone...)
	// TurnSnapshots intentionally not copied — they only matter at the
	// top of the stack, and undo pops from the live state.
	out.TurnSnapshots = nil
	return out
}
