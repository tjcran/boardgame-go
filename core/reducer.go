package core

import (
	"context"
	"errors"
	"fmt"
)

// MoveRequest names a move to apply and supplies its arguments + the player
// claiming to make it. StateID is the client's last-seen state ID; the
// reducer rejects with ErrStaleState when it doesn't match (unless the
// move opted in via IgnoreStaleStateID). StateID=0 disables the check.
type MoveRequest struct {
	PlayerID string `json:"playerID"`
	Move     string `json:"move"`
	Args     []any  `json:"args"`
	StateID  int    `json:"stateID,omitempty"`
}

// Public sentinel errors. They're surfaced through the transport with
// matching HTTP statuses.
var (
	ErrInvalidMove    = errors.New("invalid move")
	ErrWrongPlayer    = errors.New("not your turn")
	ErrUnknownMove    = errors.New("unknown move")
	ErrGameOver       = errors.New("game is over")
	ErrMinMoves       = errors.New("minimum moves not reached")
	ErrInactivePlayer = errors.New("player is not active")
	ErrStaleState     = errors.New("client state is stale")
)

// Apply runs a move through the full reducer pipeline with a
// context.Background() context. For request-scoped propagation use
// ApplyContext.
func Apply(game *Game, state State, req MoveRequest) (State, error) {
	return ApplyContext(context.Background(), game, state, req)
}

// ApplyContext is the context-aware variant of Apply. The context is
// installed on MoveContext.Context so moves can honour cancellation and
// deadlines. The reducer itself does not check ctx.Done — moves and
// plugins are responsible for that, since the reducer is fast (the
// expensive work is the user's code).
//
//  1. Reject if the game is already over.
//  2. Check the player is allowed to move in the current scope.
//  3. Resolve the move from the active phase or global table.
//  4. Run the move function -> new G.
//  5. Run turn.OnMove and count the move (unless NoLimit).
//  6. Drain queued events from the move (endTurn, setStage, ...).
//  7. Check turn.EndIf, turn.MaxMoves -> auto-end turn.
//  8. Check phase.EndIf -> auto-end phase.
//  9. Check game.EndIf -> game over.
//
// On any error, the returned state equals the input state.
func ApplyContext(ctx context.Context, game *Game, state State, req MoveRequest) (State, error) {
	if state.Ctx.Gameover != nil {
		return state, ErrGameOver
	}

	// Player must be allowed to move in this scope.
	stage, err := authorizedStage(state.Ctx, req.PlayerID)
	if err != nil {
		return state, err
	}

	// Resolve the move from the active move table, honouring stage
	// overrides first, then phase overrides, then global.
	move, err := resolveMove(game, state.Ctx, stage, req.Move)
	if err != nil {
		return state, err
	}

	events := &Events{}
	plugins := buildPluginAPIs(game, state, req.PlayerID)
	mc := &MoveContext{
		G:        state.G,
		Ctx:      state.Ctx,
		PlayerID: req.PlayerID,
		Events:   events,
		Plugins:  plugins,
		Context:  ctx,
	}
	// Ergonomic shortcut: if the Random plugin is registered, expose its
	// API as mc.Random so moves can write `mc.Random.D6()` instead of
	// `mc.Plugins["random"].(*core.Random).D6()`.
	if r, ok := plugins[RandomPluginName].(*Random); ok {
		mc.Random = r
	}

	// Stale-state guard. Opt-in: req.StateID=0 means "don't check"
	// (server-internal callers pass 0 because they always have the latest
	// state). Real clients send the StateID they last received; if it
	// doesn't match the authoritative one, the move is rejected unless
	// the move sets IgnoreStaleStateID.
	if req.StateID > 0 && req.StateID != state.StateID && !move.IgnoreStaleStateID {
		return state, ErrStaleState
	}

	// Snapshot pre-move state for undo. Only meaningful when the game
	// hasn't disabled undo and the specific move is undoable. We resolve
	// Undoable now so undo decisions reflect the state at move time.
	undoable := move.IsUndoable(mc) && !game.DisableUndo
	var snapshot State
	if undoable {
		snapshot = cloneStateForSnapshot(state)
	}
	redact := move.IsRedacted(mc)

	moveFn := applyFnWrapMove(game, move.Move)
	nextG, err := moveFn(mc, req.Args...)
	if err != nil {
		return state, err
	}

	next := state
	next.G = nextG
	next.StateID = state.StateID + 1
	mc.G = next.G // hooks see the post-move G

	// Append to the log. Args are kept; PlayerView redacts to other seats.
	next.Log = append(next.Log, LogEntry{
		Kind:     "move",
		Move:     req.Move,
		PlayerID: req.PlayerID,
		Args:     append([]any(nil), req.Args...),
		Turn:     state.Ctx.Turn,
		Phase:    state.Ctx.Phase,
		Redact:   redact,
		Undoable: undoable,
	})
	// Any successful move invalidates the redo stack.
	next.Undone = nil
	if undoable {
		next.TurnSnapshots = append(next.TurnSnapshots, snapshot)
	}

	// Persist plugin mutations into State.Plugins.
	next = flushPlugins(game, next, mc)

	// Reject the move if any plugin signals invalidity (BGIO's isInvalid).
	if err := validatePlugins(game, next); err != nil {
		return state, err
	}

	// Run turn.OnMove with the updated G.
	if turn := game.scopeTurn(next.Ctx.Phase); turn != nil && turn.OnMove != nil {
		next.G = applyFnWrapHook(game, turn.OnMove, GameMethodTurnOnMove)(mc)
		mc.G = next.G
	}

	// Count this move unless the move opted out.
	if !move.NoLimit {
		next.Ctx.NumMoves++
		next = bumpStageMoveCount(next, req.PlayerID)
	}

	// Drain events queued from the move + onMove first. These are explicit
	// transitions the move asked for (events.EndTurn, events.SetStage, …).
	next, err = drainEvents(game, next, mc, events)
	if err != nil {
		return state, err
	}

	// BGIO order: Game.EndIf is evaluated BEFORE any auto-end behaviour
	// (so ctx.CurrentPlayer in EndIf is the player who just moved). Then
	// phase.EndIf, then per-stage maxMoves cleanup, then turn.EndIf/maxMoves.
	if next.Ctx.Gameover == nil && game.EndIf != nil {
		mc2 := &MoveContext{G: next.G, Ctx: next.Ctx, PlayerID: req.PlayerID, Events: events}
		if res := game.EndIf(mc2); res != nil {
			next.Ctx.Gameover = res
			next = runGameOnEnd(game, next, events)
		}
	}

	if next.Ctx.Gameover == nil {
		next = checkPhaseEndIf(game, next, events)
	}

	if next.Ctx.Gameover == nil {
		next = autoEndStagesByMaxMoves(next)
	}

	if next.Ctx.Gameover == nil {
		next = checkTurnAutoEnd(game, next, move, events)
	}

	// Drain anything queued by the EndIf / auto-end paths above.
	next, err = drainEvents(game, next, mc, events)
	if err != nil {
		return state, err
	}

	// Flush any AddLog entries that hooks/moves appended.
	if mc.extra != nil {
		next.Log = append(next.Log, mc.extra.entries...)
	}

	return next, nil
}

// authorizedStage returns the stage name the player is currently in, or
// errors if they're not allowed to move right now.
//
// Rules (per BGIO):
//   - If ctx.ActivePlayers is non-nil, only listed players may move; their
//     stage is the map value.
//   - Otherwise only ctx.CurrentPlayer may move.
func authorizedStage(ctx Ctx, playerID string) (string, error) {
	if ctx.ActivePlayers != nil {
		stage, ok := ctx.ActivePlayers[playerID]
		if !ok {
			return "", fmt.Errorf("%w: %s", ErrInactivePlayer, playerID)
		}
		return stage, nil
	}
	if playerID != ctx.CurrentPlayer {
		return "", fmt.Errorf("%w: current=%s got=%s",
			ErrWrongPlayer, ctx.CurrentPlayer, playerID)
	}
	return "", nil
}

// resolveMove finds the Move for the named move in the current scope.
// Stage moves win over phase moves, which win over global moves.
func resolveMove(game *Game, ctx Ctx, stage, name string) (Move, error) {
	// 1. Stage moves (only if a stage is active).
	if stage != "" && ctx.Phase != "" {
		if p, ok := game.Phases[ctx.Phase]; ok {
			if p.Turn != nil {
				if s, ok := p.Turn.Stages[stage]; ok && s.Moves != nil {
					if v, ok := s.Moves[name]; ok {
						return asMove(v)
					}
				}
			}
		}
	}
	if stage != "" && game.Turn != nil {
		if s, ok := game.Turn.Stages[stage]; ok && s.Moves != nil {
			if v, ok := s.Moves[name]; ok {
				return asMove(v)
			}
		}
	}
	// 2. Phase moves (or global if phase didn't override).
	scope := game.scopeMoves(ctx.Phase)
	if v, ok := scope[name]; ok {
		return asMove(v)
	}
	return Move{}, fmt.Errorf("%w: %q", ErrUnknownMove, name)
}

// checkTurnAutoEnd evaluates turn.EndIf and turn.MaxMoves and ends the turn
// if either fires. events is the shared queue from the outer drain loop.
func checkTurnAutoEnd(game *Game, state State, move Move, events *Events) State {
	turn := game.scopeTurn(state.Ctx.Phase)
	if turn == nil {
		return state
	}
	if turn.EndIf != nil {
		mc := &MoveContext{G: state.G, Ctx: state.Ctx, Events: events}
		if end, next := turn.EndIf(mc); end {
			return endTurn(game, state, next, events)
		}
	}
	if turn.MaxMoves > 0 && state.Ctx.NumMoves >= turn.MaxMoves && !move.NoLimit {
		return endTurn(game, state, "", events)
	}
	return state
}

// checkPhaseEndIf evaluates phase.EndIf for the current phase and rotates
// phases if it fires.
func checkPhaseEndIf(game *Game, state State, events *Events) State {
	if state.Ctx.Phase == "" {
		return state
	}
	p, ok := game.Phases[state.Ctx.Phase]
	if !ok || p.EndIf == nil {
		return state
	}
	mc := &MoveContext{G: state.G, Ctx: state.Ctx, Events: events}
	end, next := p.EndIf(mc)
	if !end {
		return state
	}
	return endPhase(game, state, next, events)
}

// bumpStageMoveCount records a move against the per-active-player counter
// used for stage-level Min/MaxMoves.
func bumpStageMoveCount(state State, playerID string) State {
	if state.Ctx.ActivePlayers == nil {
		return state
	}
	if _, ok := state.Ctx.ActivePlayers[playerID]; !ok {
		return state
	}
	if state.MoveCounts == nil {
		state.MoveCounts = map[string]int{}
	}
	state.MoveCounts[playerID]++
	return state
}

// autoEndStagesByMaxMoves removes from ctx.ActivePlayers any player whose
// stage MaxMoves has been reached. If the active set drains, applies any
// pending revert / next config.
func autoEndStagesByMaxMoves(state State) State {
	if state.Ctx.ActivePlayers == nil {
		return state
	}
	changed := false
	for pid := range state.Ctx.ActivePlayers {
		max, ok := state.StageMaxMoves[pid]
		if !ok || max <= 0 {
			continue
		}
		if state.MoveCounts[pid] >= max {
			delete(state.Ctx.ActivePlayers, pid)
			if state.StageMinMoves != nil {
				delete(state.StageMinMoves, pid)
			}
			if state.StageMaxMoves != nil {
				delete(state.StageMaxMoves, pid)
			}
			if state.MoveCounts != nil {
				delete(state.MoveCounts, pid)
			}
			changed = true
		}
	}
	if changed && len(state.Ctx.ActivePlayers) == 0 {
		state = drainActivePlayers(state)
	}
	return state
}

// drainActivePlayers lives in transitions.go (the file that owns most of
// the active-players state machine). It's referenced from autoEndStagesByMaxMoves
// below.
