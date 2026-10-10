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
//
// ResumeTag, when set, matches against State.Blocks at Apply entry —
// the first BlockSpec with matching Tag + PlayerID (and Move, when the
// block names its answer) is removed before the move runs. Used to
// resolve pause points opened by an earlier cascade's Queue.Block call.
type MoveRequest struct {
	PlayerID  string `json:"playerID"`
	Move      string `json:"move"`
	Args      []any  `json:"args"`
	StateID   int    `json:"stateID,omitempty"`
	ResumeTag string `json:"resumeTag,omitempty"`
	// NowMs is the request's wall-clock timestamp (Unix ms). The match
	// manager stamps it when zero; Replay passes the value recorded in
	// the move log so time-reading games replay deterministically.
	NowMs int64 `json:"nowMs,omitempty"`
	// ServerDispatch marks a move the server dispatched on its own
	// authority (match.Manager.DispatchServer) rather than one a client
	// sent. Such a move is not confined by an Exclusive stage's move
	// table, the same as drain steps and Events.RunMove: the server's
	// authority covers the game's whole move set. The seat named by
	// PlayerID must still be allowed to move. Never decoded from the
	// wire; the reducer records it on the move's log entry so Replay
	// and Redo re-apply the move the same way.
	ServerDispatch bool `json:"-"`
	// FromClient marks a request a client sent through the match
	// manager's credentialed ingress (Manager.MoveReqCtx and
	// Manager.DryMoveReq set it). Such a request may not make a move
	// flagged Move.ServerOnly, wherever that move is registered: the
	// reducer judges the move it resolved for the request and refuses
	// with ErrServerOnly. In-process callers (tests, bots, Replay) leave
	// it off and keep the server's authority. Never decoded from the
	// wire and not logged, because the refusal is an ingress check that
	// a recorded move has already passed.
	FromClient bool `json:"-"`
}

// Public sentinel errors. They're surfaced through the transport with
// matching HTTP statuses.
var (
	ErrInvalidMove      = errors.New("invalid move")
	ErrWrongPlayer      = errors.New("not your turn")
	ErrUnknownMove      = errors.New("unknown move")
	ErrGameOver         = errors.New("game is over")
	ErrMinMoves         = errors.New("minimum moves not reached")
	ErrInactivePlayer   = errors.New("player is not active")
	ErrStaleState       = errors.New("client state is stale")
	ErrBlocked          = errors.New("match has pending blocks; supply MoveRequest.ResumeTag")
	ErrUnknownResumeTag = errors.New("ResumeTag does not match any pending block")
	ErrMoveNotInStage   = errors.New("move is not allowed in the player's current stage")
	ErrServerOnly       = errors.New("move is marked Move.ServerOnly — credentialed clients cannot dispatch it; use Manager.DispatchServer")
	ErrDrainOverflow    = errors.New("cascade drain exceeded MaxDrainDepth")
)

// MaxDrainDepth caps how many drain steps the reducer will run for a
// single external move. Exceeding the cap rolls the entire cascade
// back to the pre-external state and returns ErrDrainOverflow. 200 is
// a comfortable margin above any plausible TCG resolution depth.
const MaxDrainDepth = 200

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
//  2. Resolve ResumeTag against State.Blocks (removes one matching).
//  3. Resolve the move from the player's stage table, then the active
//     phase or global table. Steps 4 to 8 all judge this one move.
//  4. A client's request (FromClient) may not make a ServerOnly move
//     (ErrServerOnly).
//  5. If blocks remain, no ResumeTag was consumed and the move doesn't
//     IgnoreBlocks, ErrBlocked.
//  6. Check the player is allowed to move in the current scope.
//  7. Reject a stale StateID (ErrStaleState).
//  8. An Exclusive stage refuses a move that did not resolve from its
//     own table (ErrMoveNotInStage).
//  9. Run the move function -> new G.
//  10. Run turn.OnMove and count the move (unless NoLimit).
//  11. Drain queued events (endTurn, setStage, ...).
//  12. Check Game.EndIf, phase.EndIf, turn.EndIf / MaxMoves.
//  13. Bump State.StateID once.
//  14. Drain State.Queue (cascade). Each drain step runs through
//     applyOne with a Parent log index; the outer state-ID stays put.
//     Pauses on the first non-empty Blocks set.
//
// Which requests skip steps 6 and 8 is decided in one place,
// exemptionsFor.
//
// On any error, whichever step raised it, ApplyContext returns the state
// it was given (cascades are atomic). The reducer never writes into that
// state: before the move runs it takes private copies of the containers
// it changes in place (see ownContainers). Two things stay shared. G and
// plugin data: a move or plugin that mutates them in place, rather than
// returning new values, writes through to the caller's state even when
// the move fails. And the spare capacity of the slices the reducer
// appends to (Log, Queue, Blocks, TurnSnapshots): results of two Apply
// calls on one input can share a backing array, so a caller that keeps
// both should first cut those slices to their length
// (s.Log = s.Log[:len(s.Log):len(s.Log)], and so on).
func ApplyContext(ctx context.Context, game *Game, state State, req MoveRequest) (State, error) {
	next, err := applyExternal(ctx, game, state, req)
	if err != nil {
		return state, err
	}
	return next, nil
}

// applyExternal is ApplyContext's pipeline. state is a value copy of the
// caller's state: the checks only reassign its fields, and it takes its
// own containers before the move runs. On error the returned State is
// meaningless, because ApplyContext discards it and hands back the
// caller's state.
func applyExternal(ctx context.Context, game *Game, state State, req MoveRequest) (State, error) {
	if state.Ctx.Gameover != nil {
		return State{}, ErrGameOver
	}

	// Expose the request's wall clock to moves and hooks for this apply.
	state.Ctx.NowMs = req.NowMs

	// Resume-tag handling: remove a matching block before the move runs.
	// If no match, refuse — the tag implies the caller thinks they're
	// resolving a specific pause that doesn't exist. The consumed block
	// is stashed in `resumingBlock` so the move can read what it's
	// resuming (e.g. ResumingBlock.Target for typed target requests).
	var resumingBlock *BlockSpec
	if req.ResumeTag != "" {
		idx := findBlock(state.Blocks, req.ResumeTag, req.PlayerID, req.Move)
		if idx < 0 {
			return State{}, fmt.Errorf("%w: tag=%s player=%s move=%s",
				ErrUnknownResumeTag, req.ResumeTag, req.PlayerID, req.Move)
		}
		b := state.Blocks[idx]
		resumingBlock = &b
		// Fresh slice: removing in place would shift the backing array
		// the caller's state shares (ownContainers does not copy Blocks).
		kept := make([]BlockSpec, 0, len(state.Blocks)-1)
		kept = append(kept, state.Blocks[:idx]...)
		state.Blocks = append(kept, state.Blocks[idx+1:]...)
	}

	// Resolve the move once, from the caller's stage table first, then
	// the phase's table, then the game's. Every check below judges this
	// move, so none of them can disagree about which move would run. A
	// player authorizedStage refuses is not listed in ActivePlayers, so
	// their stage is "" and an exempt move resolves from the phase/global
	// table.
	stage, authErr := authorizedStage(state.Ctx, req.PlayerID)
	move, inStage, err := resolveMove(game, state.Ctx, stage, req.Move)

	// A client may not make a ServerOnly move, wherever it is registered.
	if req.FromClient && err == nil && move.ServerOnly {
		return State{}, ErrServerOnly
	}

	// Block gate: pending blocks refuse non-IgnoreBlocks moves that are
	// not resuming one of them, so unrelated external work can't sneak
	// past a pause. A move that consumed a valid ResumeTag proceeds even
	// when other blocks remain — a cascade can raise several prompts at
	// once, and they must be answerable one at a time (gating resumes on
	// "all blocks gone" would soft-lock the match; the remaining blocks
	// keep gating every non-resume move).
	if resumingBlock == nil && len(state.Blocks) > 0 && err == nil && !move.IgnoreBlocks {
		return State{}, ErrBlocked
	}

	// Player must be allowed to move in this scope. exemptionsFor says
	// which refusals a request is excused from.
	exempt := exemptionsFor(move, resumingBlock, req)
	if authErr != nil && (err != nil || !exempt.turn) {
		return State{}, authErr
	}
	if err != nil {
		return State{}, err
	}

	// Stale-state guard. Opt-in: req.StateID=0 means "don't check"
	// (server-internal callers pass 0 because they always have the latest
	// state). Real clients send the StateID they last received; if it
	// doesn't match the authoritative one, the move is rejected unless
	// the move sets IgnoreStaleStateID. Checked before stage scoping so a
	// client acting on an old view is told to refresh rather than that
	// its move is illegal in a stage it may no longer be in.
	if req.StateID > 0 && req.StateID != state.StateID && !move.IgnoreStaleStateID {
		return State{}, ErrStaleState
	}

	// An Exclusive stage confines its players to its own move table,
	// so a name that resolved from the phase/global fallback is refused.
	// resolveMove read the stage table from the same lookupStage config
	// whose Exclusive flag is checked here.
	if stage != "" && !inStage && !exempt.stageScope {
		if sc := lookupStage(game, state.Ctx.Phase, stage); sc != nil && sc.Exclusive {
			return State{}, fmt.Errorf("%w: move=%q stage=%q",
				ErrMoveNotInStage, req.Move, stage)
		}
	}

	// The checks above only reassign fields of this value copy. From here
	// on the pipeline writes into containers in place, so take private
	// copies now: a request refused above costs no copying.
	state = ownContainers(state)

	events := &Events{}
	queue := &Queue{}
	plugins := buildPluginAPIs(game, state, req.PlayerID)
	moveCtx := ctx
	if move.Timeout > 0 {
		var cancel context.CancelFunc
		moveCtx, cancel = context.WithTimeout(ctx, move.Timeout)
		defer cancel()
	}
	mc := (&MoveContext{
		G:             state.G,
		Ctx:           state.Ctx,
		PlayerID:      req.PlayerID,
		Events:        events,
		Context:       moveCtx,
		Queue:         queue,
		ResumingBlock: resumingBlock,
	}).attachPlugins(plugins)
	// Hooks fired during this move share the move's plugin APIs, so their
	// mutations land in the same objects flushPlugins persists below.
	env := &hookEnv{events: events, plugins: plugins}

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
		return State{}, err
	}

	next := state
	next.G = nextG
	next.StateID = state.StateID + 1
	mc.G = next.G // hooks see the post-move G

	// Append to the log. Args are kept; PlayerView redacts to other seats.
	parentIdx := len(next.Log)
	next.Log = append(next.Log, LogEntry{
		Kind:           "move",
		Move:           req.Move,
		PlayerID:       req.PlayerID,
		Args:           append([]any(nil), req.Args...),
		Turn:           state.Ctx.Turn,
		Phase:          state.Ctx.Phase,
		Stage:          state.Ctx.ActivePlayers[req.PlayerID],
		Redact:         redact,
		Undoable:       undoable,
		Parent:         -1,
		ResumeTag:      req.ResumeTag,
		NowMs:          req.NowMs,
		ServerDispatch: req.ServerDispatch,
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
		return State{}, err
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
	next, err = drainEvents(game, next, mc, env)
	if err != nil {
		return State{}, err
	}

	// BGIO order: Game.EndIf is evaluated BEFORE any auto-end behaviour
	// (so ctx.CurrentPlayer in EndIf is the player who just moved). Then
	// phase.EndIf, then per-stage maxMoves cleanup, then turn.EndIf/maxMoves.
	if next.Ctx.Gameover == nil && game.EndIf != nil {
		mc2 := env.mc(next, req.PlayerID)
		if res := game.EndIf(mc2); res != nil {
			next.Ctx.Gameover = res
			next = runGameOnEnd(game, next, env)
		}
	}

	if next.Ctx.Gameover == nil {
		next = checkPhaseEndIf(game, next, env)
	}

	if next.Ctx.Gameover == nil {
		next = autoEndStagesByMaxMoves(next)
	}

	if next.Ctx.Gameover == nil {
		next = checkTurnAutoEnd(game, next, move, env)
	}

	// Drain anything queued by the EndIf / auto-end paths above.
	next, err = drainEvents(game, next, mc, env)
	if err != nil {
		return State{}, err
	}

	// Flush any AddLog entries that hooks/moves appended.
	if mc.extra != nil {
		next.Log = append(next.Log, mc.extra.entries...)
	}

	// Apply DropBlocksFor requests BEFORE merging new blocks: a
	// timeout/forfeit move abandons the named players' stranded
	// prompts (their pending effects die with them).
	if len(mc.dropBlocksFor) > 0 {
		drop := map[string]bool{}
		for _, pid := range mc.dropBlocksFor {
			drop[pid] = true
		}
		// Fresh slice — next.Blocks may alias the caller's array, and
		// the undo snapshot's.
		kept := make([]BlockSpec, 0, len(next.Blocks))
		for _, b := range next.Blocks {
			if !drop[b.PlayerID] {
				kept = append(kept, b)
			}
		}
		next.Blocks = kept
	}

	// Harvest the move's Queue.Push / Queue.Block calls.
	pending, newBlocks := queue.drain()
	next.Queue = append(next.Queue, pending...)
	next.Blocks = append(next.Blocks, newBlocks...)

	// Cascade drain. Each pending action runs through applyStep (the
	// same machinery as Apply but no state-ID bump, no resume-tag
	// handling, no ErrBlocked gate — those are external-move concerns).
	// Pauses on the first non-empty Blocks set. Any error inside the
	// cascade rolls back the WHOLE external move so we never persist a
	// half-finished resolution.
	depth := 0
	for len(next.Queue) > 0 && len(next.Blocks) == 0 && next.Ctx.Gameover == nil {
		depth++
		if depth > MaxDrainDepth {
			return State{}, ErrDrainOverflow
		}
		step := next.Queue[0]
		next.Queue = next.Queue[1:]
		var stepErr error
		next, stepErr = applyStep(ctx, game, next, step, parentIdx)
		if stepErr != nil {
			return State{}, stepErr
		}
	}

	return next, nil
}

// findBlock returns the index of the first BlockSpec matching tag +
// playerID that move may answer, or -1 when no match exists. A block
// that names its answer (BlockSpec.Move) matches only that move.
func findBlock(blocks []BlockSpec, tag, playerID, move string) int {
	for i, b := range blocks {
		if b.Tag == tag && b.PlayerID == playerID && (b.Move == "" || b.Move == move) {
			return i
		}
	}
	return -1
}

// exemptions records which per-player checks in ApplyContext a request
// is excused from.
type exemptions struct {
	// turn: the caller may move although authorizedStage refuses them
	// (not their turn, or not listed in ActivePlayers).
	turn bool
	// stageScope: a caller in an Exclusive stage may make a move that is
	// not in the stage's own table.
	stageScope bool
}

// exemptionsFor is the single statement of who may bypass the turn
// check and an Exclusive stage's move table. move is the resolved move,
// resuming the block this request consumed (nil when none).
//
// Both checks excuse AnyPlayer moves (concede / forfeit / forcing an
// opponent's timeout): any seat may make them at any time, so neither
// the turn nor a stage may refuse them. The other exemptions differ,
// because the two checks ask different questions — whether this seat
// may act now, and which moves a seat that may act can make:
//
//   - A consumed block excuses the turn check. A block names the one
//     player who may answer it and findBlock matches on PlayerID, so the
//     caller is the player the engine asked; refusing them out of turn
//     would deadlock the match, since every other seat is held off by
//     ErrBlocked on the same block. A block that does not name its
//     answer move can be consumed by any move carrying its tag, so this
//     is as wide as it was before blocks could name their answer.
//   - Stage scoping excuses only a block that names its answer move
//     (AnsweredBy); findBlock has already checked that this request is
//     that move. An unnamed block proves nothing about the move — any
//     move could carry its tag — so its answer must be in the stage's
//     table like any other move.
//   - IgnoreBlocks moves (concede / forfeit / emergency exit) excuse
//     stage scoping: a seat that may act must not be stranded in a
//     stage without its escape hatch. They do not excuse the turn
//     check; an escape hatch that must work out of turn is AnyPlayer.
//   - Server-dispatched moves excuse stage scoping: the server's
//     authority covers the whole move set, as it does for drain steps
//     and Events.RunMove. They do not excuse the turn check, because
//     DispatchServer acts as a seat and validates like that seat's own
//     move.
func exemptionsFor(move Move, resuming *BlockSpec, req MoveRequest) exemptions {
	namedAnswer := resuming != nil && resuming.Move != ""
	return exemptions{
		turn:       move.AnyPlayer || resuming != nil,
		stageScope: move.AnyPlayer || move.IgnoreBlocks || namedAnswer || req.ServerDispatch,
	}
}

// applyStep runs a server-driven move from State.Queue. Same pipeline
// as ApplyContext for an external move, with three differences:
//
//   - No state-ID bump (state-ID is bumped once per external move).
//   - No ResumeTag / ErrBlocked handling — drain steps run regardless.
//   - The log entry's Parent points at the external move's index.
//
// Errors here bubble up to ApplyContext, which rolls the whole cascade
// back. Drain steps that want to fail silently should return mc.G
// unchanged.
func applyStep(ctx context.Context, game *Game, state State, action QueuedAction, parentIdx int) (State, error) {
	stage, err := authorizedStageDrain(state.Ctx, action.PlayerID)
	if err != nil {
		return state, err
	}
	move, _, err := resolveMove(game, state.Ctx, stage, action.Move)
	if err != nil {
		return state, err
	}

	events := &Events{}
	queue := &Queue{}
	plugins := buildPluginAPIs(game, state, action.PlayerID)
	mc := (&MoveContext{
		G:        state.G,
		Ctx:      state.Ctx,
		PlayerID: action.PlayerID,
		Events:   events,
		Context:  ctx,
		Queue:    queue,
	}).attachPlugins(plugins)
	env := &hookEnv{events: events, plugins: plugins}

	moveFn := applyFnWrapMove(game, move.Move)
	nextG, err := moveFn(mc, action.Args...)
	if err != nil {
		return state, err
	}

	next := state
	next.G = nextG
	mc.G = next.G
	next.Log = append(next.Log, LogEntry{
		Kind:     "drain-step",
		Move:     action.Move,
		PlayerID: action.PlayerID,
		Args:     append([]any(nil), action.Args...),
		Turn:     state.Ctx.Turn,
		Phase:    state.Ctx.Phase,
		Stage:    stage,
		Parent:   parentIdx,
	})
	next = flushPlugins(game, next, mc)
	if err := validatePlugins(game, next); err != nil {
		return state, err
	}
	next, err = drainEvents(game, next, mc, env)
	if err != nil {
		return state, err
	}
	if mc.extra != nil {
		next.Log = append(next.Log, mc.extra.entries...)
	}
	pending, newBlocks := queue.drain()
	next.Queue = append(next.Queue, pending...)
	next.Blocks = append(next.Blocks, newBlocks...)
	return next, nil
}

// authorizedStageDrain is the same as authorizedStage but tolerates an
// active-players mismatch — drain steps are server-driven and not
// subject to the same gating as a client move. The stage lookup is
// still useful for resolving stage-scoped move tables.
func authorizedStageDrain(ctx Ctx, playerID string) (string, error) {
	if ctx.ActivePlayers != nil {
		if stage, ok := ctx.ActivePlayers[playerID]; ok {
			return stage, nil
		}
	}
	return "", nil
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
// Stage moves win over phase moves, which win over global moves. The
// stage's moves are the Moves of the stage config lookupStage picks (the
// active phase's Turn.Stages entry, else the game-level one), the same
// config whose Exclusive flag and hooks govern the stage. inStage
// reports whether the move came from that table rather than the
// phase/global fallback; it is the one statement of which moves an
// Exclusive stage holds.
func resolveMove(game *Game, ctx Ctx, stage, name string) (move Move, inStage bool, err error) {
	if stage != "" {
		if sc := lookupStage(game, ctx.Phase, stage); sc != nil {
			if v, ok := sc.Moves[name]; ok {
				move, err = asMove(v)
				return move, true, err
			}
		}
	}
	if v, ok := game.scopeMoves(ctx.Phase)[name]; ok {
		move, err = asMove(v)
		return move, false, err
	}
	return Move{}, false, fmt.Errorf("%w: %q", ErrUnknownMove, name)
}

// checkTurnAutoEnd evaluates turn.EndIf and turn.MaxMoves and ends the turn
// if either fires. env carries the shared queue from the outer drain loop.
func checkTurnAutoEnd(game *Game, state State, move Move, env *hookEnv) State {
	turn := game.scopeTurn(state.Ctx.Phase)
	if turn == nil {
		return state
	}
	if turn.EndIf != nil {
		mc := env.mc(state, "")
		if end, next := turn.EndIf(mc); end {
			return endTurn(game, state, next, env)
		}
	}
	if turn.MaxMoves > 0 && state.Ctx.NumMoves >= turn.MaxMoves && !move.NoLimit {
		return endTurn(game, state, "", env)
	}
	return state
}

// checkPhaseEndIf evaluates phase.EndIf for the current phase and rotates
// phases if it fires.
func checkPhaseEndIf(game *Game, state State, env *hookEnv) State {
	if state.Ctx.Phase == "" {
		return state
	}
	p, ok := game.Phases[state.Ctx.Phase]
	if !ok || p.EndIf == nil {
		return state
	}
	mc := env.mc(state, "")
	end, next := p.EndIf(mc)
	if !end {
		return state
	}
	return endPhase(game, state, next, env)
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
