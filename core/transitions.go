package core

// This file holds the engine-internal state transition helpers used by the
// reducer: turn rotation, phase rotation, stage rotation, drainEvents,
// active-player set construction.
//
// All hooks share a single Events queue with their caller, so events
// queued inside a hook (e.g. phase.OnBegin calling events.EndTurn) are
// picked up by the surrounding drain loop. BGIO has a bug (#1237) where
// these are silently dropped — we explicitly fix it.

// drainEvents flushes the events queue in order, applying each transition.
// Some events can in turn enqueue more events (e.g. an OnEnd hook calling
// SetPhase), so this drains until the queue is empty.
func drainEvents(game *Game, state State, mc *MoveContext, events *Events) (State, error) {
	for {
		batch := events.drain()
		if len(batch) == 0 {
			return state, nil
		}
		for _, ev := range batch {
			switch ev.kind {
			case evEndTurn, evPass:
				// MinMoves: silently drop the EndTurn if the player hasn't
				// played enough counting moves yet. BGIO does the same —
				// see api/Game.md: "Prevents ending the turn before a
				// minimum number of moves."
				if turn := game.scopeTurn(state.Ctx.Phase); turn != nil &&
					turn.MinMoves > 0 && state.Ctx.NumMoves < turn.MinMoves {
					continue
				}
				state = endTurn(game, state, ev.turnNext, events)
			case evEndPhase:
				state = endPhase(game, state, "", events)
			case evSetPhase:
				state = endPhase(game, state, ev.phase, events)
			case evEndStage:
				state = endStage(game, state, mc.PlayerID, events)
			case evSetStage:
				if ev.stageOpts != nil {
					state = setStageLong(game, state, mc.PlayerID, ev.stageOpts, events)
				} else {
					state = setStage(game, state, mc.PlayerID, ev.stage, events)
				}
			case evEndGame:
				state.Ctx.Gameover = ev.gameover
				state = runGameOnEnd(game, state, events)
			case evSetActivePlayers:
				if ev.activeCfg != nil {
					state = applySetActivePlayers(state, *ev.activeCfg)
				}
			case evRemovePlayer:
				state = removePlayer(game, state, ev.playerID, events)
			}
			// Refresh the working context so subsequent hooks in this
			// drain see the latest state.
			mc.G = state.G
			mc.Ctx = state.Ctx
		}
	}
}

// endTurn advances to the next turn, running OnEnd / Order.Next / OnBegin.
// nextPlayer, if non-empty, forces that player to be the new CurrentPlayer
// instead of using the turn order (parity with BGIO's endTurn({next})).
//
// events is the shared queue: hooks called here queue into it and the
// outer drain loop picks them up.
func endTurn(game *Game, state State, nextPlayer string, events *Events) State {
	turn := game.scopeTurn(state.Ctx.Phase)

	// Turn.OnEnd before rotating.
	if turn != nil && turn.OnEnd != nil {
		mc := &MoveContext{G: state.G, Ctx: state.Ctx, Events: events}
		state.G = applyFnWrapHook(game, turn.OnEnd, GameMethodTurnOnEnd)(mc)
		state = flushExtraLog(state, mc)
	}

	// Clear per-turn counters and the undo scratch space — undo is
	// per-turn only (parity with BGIO).
	state.Ctx.NumMoves = 0
	state.Ctx.ActivePlayers = nil
	state.MoveCounts = nil
	state.StageMinMoves = nil
	state.StageMaxMoves = nil
	state.ActiveStack = nil
	state.PendingNext = nil
	state.TurnSnapshots = nil
	state.Undone = nil

	// Advance current player.
	if nextPlayer != "" {
		state.Ctx.CurrentPlayer = nextPlayer
		for i, p := range state.Ctx.PlayOrder {
			if p == nextPlayer {
				state.Ctx.PlayOrderPos = i
				break
			}
		}
	} else {
		state = advancePlayOrderPos(game, state, turn, events)
		if state.Ctx.Gameover != nil {
			return state
		}
	}

	state.Ctx.Turn++

	state = applyActivePlayersFromTurn(game, state)
	state = runTurnOnBegin(game, state, events)
	return state
}

// advancePlayOrderPos consults the TurnOrder for the current phase to pick
// the next PlayOrderPos. If Next returns nil, the phase ends (some orders
// signal "we're done after this round").
func advancePlayOrderPos(game *Game, state State, turn *TurnConfig, events *Events) State {
	if len(state.Ctx.PlayOrder) == 0 {
		// Everyone has been eliminated. Nothing to advance to.
		return state
	}
	order := TurnOrderDefault
	if turn != nil && !turn.Order.IsDefault() {
		order = turn.Order
	}
	mc := &MoveContext{G: state.G, Ctx: state.Ctx}
	if order.Next == nil {
		state.Ctx.PlayOrderPos = (state.Ctx.PlayOrderPos + 1) % len(state.Ctx.PlayOrder)
	} else {
		n := order.Next(mc)
		if n == nil {
			return endPhase(game, state, "", events)
		}
		state.Ctx.PlayOrderPos = *n
		if state.Ctx.PlayOrderPos >= len(state.Ctx.PlayOrder) {
			state.Ctx.PlayOrderPos = state.Ctx.PlayOrderPos % len(state.Ctx.PlayOrder)
		}
	}
	state.Ctx.CurrentPlayer = state.Ctx.PlayOrder[state.Ctx.PlayOrderPos]
	return state
}

// applyTurnOrderFirst is called when entering a (new) phase. It applies the
// PlayOrder override (if any) and sets the initial PlayOrderPos.
func applyTurnOrderFirst(game *Game, state State, mc *MoveContext) State {
	turn := game.scopeTurn(state.Ctx.Phase)
	order := TurnOrderDefault
	if turn != nil && !turn.Order.IsDefault() {
		order = turn.Order
	}
	if order.PlayOrder != nil {
		state.Ctx.PlayOrder = order.PlayOrder(mc)
		state.Ctx.NumPlayers = len(state.Ctx.PlayOrder)
	}
	if order.First != nil {
		state.Ctx.PlayOrderPos = order.First(mc)
	} else {
		state.Ctx.PlayOrderPos = 0
	}
	if len(state.Ctx.PlayOrder) > 0 {
		state.Ctx.CurrentPlayer = state.Ctx.PlayOrder[state.Ctx.PlayOrderPos]
	}
	return state
}

// endPhase ends the current phase, runs OnEnd, transitions to nextPhase (or
// the phase's static Next), runs OnBegin.
func endPhase(game *Game, state State, nextPhase string, events *Events) State {
	if state.Ctx.Phase == "" {
		if nextPhase != "" {
			state.Ctx.Phase = nextPhase
			state = runPhaseEnter(game, state, events)
		}
		return state
	}
	current, ok := game.Phases[state.Ctx.Phase]
	if !ok {
		return state
	}

	// Whenever a phase ends, the current player's turn is first ended.
	if turn := game.scopeTurn(state.Ctx.Phase); turn != nil && turn.OnEnd != nil {
		mc := &MoveContext{G: state.G, Ctx: state.Ctx, Events: events}
		state.G = applyFnWrapHook(game, turn.OnEnd, GameMethodTurnOnEnd)(mc)
		state = flushExtraLog(state, mc)
	}

	// Resolve next phase.
	target := nextPhase
	if target == "" {
		mc := &MoveContext{G: state.G, Ctx: state.Ctx}
		target = current.resolveNextPhase(mc)
	}

	// OnEnd of the phase we're leaving.
	if current.OnEnd != nil {
		mc := &MoveContext{G: state.G, Ctx: state.Ctx, Events: events}
		state.G = applyFnWrapHook(game, current.OnEnd, GameMethodPhaseOnEnd)(mc)
		state = flushExtraLog(state, mc)
	}

	// Clear turn-scoped state.
	state.Ctx.NumMoves = 0
	state.Ctx.ActivePlayers = nil
	state.MoveCounts = nil
	state.StageMinMoves = nil
	state.StageMaxMoves = nil
	state.ActiveStack = nil
	state.PendingNext = nil

	state.Ctx.Phase = target
	state.Ctx.Turn++

	if target == "" {
		state = advancePlayOrderPos(game, state, game.Turn, events)
		state = applyActivePlayersFromTurn(game, state)
		state = runTurnOnBegin(game, state, events)
		return state
	}
	state = runPhaseEnter(game, state, events)
	return state
}

// runPhaseEnter runs the entry sequence for the phase named in state.Ctx.Phase.
// Used both for the starting phase (from NewMatch) and for transitions.
func runPhaseEnter(game *Game, state State, events *Events) State {
	mc := &MoveContext{G: state.G, Ctx: state.Ctx, Events: events}
	state = applyTurnOrderFirst(game, state, mc)
	state = applyActivePlayersFromTurn(game, state)
	state = runPhaseOnBegin(game, state, events)
	state = runTurnOnBegin(game, state, events)
	return state
}

// runPhaseOnBegin runs the active phase's OnBegin hook, if any. Events queued
// by the hook land in `events` for the surrounding drain to pick up.
func runPhaseOnBegin(game *Game, state State, events *Events) State {
	if state.Ctx.Phase == "" {
		return state
	}
	p, ok := game.Phases[state.Ctx.Phase]
	if !ok || p.OnBegin == nil {
		return state
	}
	mc := &MoveContext{G: state.G, Ctx: state.Ctx, Events: events}
	state.G = applyFnWrapHook(game, p.OnBegin, GameMethodPhaseOnBegin)(mc)
	state = flushExtraLog(state, mc)
	return state
}

// runTurnOnBegin runs the active scope's Turn.OnBegin hook, if any.
func runTurnOnBegin(game *Game, state State, events *Events) State {
	turn := game.scopeTurn(state.Ctx.Phase)
	if turn == nil || turn.OnBegin == nil {
		return state
	}
	mc := &MoveContext{G: state.G, Ctx: state.Ctx, Events: events}
	state.G = applyFnWrapHook(game, turn.OnBegin, GameMethodTurnOnBegin)(mc)
	state = flushExtraLog(state, mc)
	return state
}

// flushExtraLog drains any entries a hook appended via mc.AddLog into the
// match log. Without this, AddLog from a hook would be silently dropped
// (BGIO issue #1228).
func flushExtraLog(state State, mc *MoveContext) State {
	if mc.extra == nil || len(mc.extra.entries) == 0 {
		return state
	}
	state.Log = append(state.Log, mc.extra.entries...)
	return state
}

// runGameOnEnd runs Game.OnEnd after the game ends. BGIO disallows events
// from this hook; we pass the queue anyway and rely on the absence of
// further calls. (No hook is fired after OnEnd, so queueing here is a
// no-op in practice.)
func runGameOnEnd(game *Game, state State, events *Events) State {
	if game.OnEnd == nil {
		return state
	}
	mc := &MoveContext{G: state.G, Ctx: state.Ctx, Events: events}
	state.G = applyFnWrapHook(game, game.OnEnd, GameMethodGameOnEnd)(mc)
	state = flushExtraLog(state, mc)
	return state
}

// setStage moves a player into a named stage. Fires stage.OnBegin if set.
func setStage(game *Game, state State, playerID, stageName string, events *Events) State {
	if state.Ctx.ActivePlayers == nil {
		state.Ctx.ActivePlayers = map[string]string{}
	}
	state.Ctx.ActivePlayers[playerID] = stageName
	if state.MoveCounts != nil {
		delete(state.MoveCounts, playerID)
	}
	// Fire stage.OnBegin if the new stage defines one.
	if stage := lookupStage(game, state.Ctx.Phase, stageName); stage != nil && stage.OnBegin != nil {
		mc := &MoveContext{G: state.G, Ctx: state.Ctx, PlayerID: playerID, Events: events}
		state.G = applyFnWrapHook(game, stage.OnBegin, GameMethodStageOnBegin)(mc)
		state = flushExtraLog(state, mc)
	}
	return state
}

// setStageLong is the long-form variant carrying per-call min/max.
func setStageLong(game *Game, state State, playerID string, opts *setStageOpts, events *Events) State {
	state = setStage(game, state, playerID, opts.stage, events)
	if opts.hasMin {
		if state.StageMinMoves == nil {
			state.StageMinMoves = map[string]int{}
		}
		state.StageMinMoves[playerID] = opts.minMoves
	}
	if opts.hasMax {
		if state.StageMaxMoves == nil {
			state.StageMaxMoves = map[string]int{}
		}
		state.StageMaxMoves[playerID] = opts.maxMoves
	}
	return state
}

// endStage removes the player from the active set (or rotates them to the
// stage's configured Next). Fires the leaving stage's OnEnd hook.
func endStage(game *Game, state State, playerID string, events *Events) State {
	if state.Ctx.ActivePlayers == nil {
		return state
	}
	currentStage := state.Ctx.ActivePlayers[playerID]

	// Fire OnEnd before mutating state.
	if stage := lookupStage(game, state.Ctx.Phase, currentStage); stage != nil && stage.OnEnd != nil {
		mc := &MoveContext{G: state.G, Ctx: state.Ctx, PlayerID: playerID, Events: events}
		state.G = applyFnWrapHook(game, stage.OnEnd, GameMethodStageOnEnd)(mc)
		state = flushExtraLog(state, mc)
	}

	// Look up stage.Next at the phase or global level.
	var nextStage string
	if state.Ctx.Phase != "" {
		if p, ok := game.Phases[state.Ctx.Phase]; ok && p.Turn != nil {
			if s, ok := p.Turn.Stages[currentStage]; ok {
				nextStage = s.Next
			}
		}
	}
	if nextStage == "" && game.Turn != nil {
		if s, ok := game.Turn.Stages[currentStage]; ok {
			nextStage = s.Next
		}
	}

	if nextStage != "" {
		// Move to the next stage (which fires that stage's OnBegin).
		state = setStage(game, state, playerID, nextStage, events)
		return state
	}

	delete(state.Ctx.ActivePlayers, playerID)
	if state.MoveCounts != nil {
		delete(state.MoveCounts, playerID)
	}
	if state.StageMinMoves != nil {
		delete(state.StageMinMoves, playerID)
	}
	if state.StageMaxMoves != nil {
		delete(state.StageMaxMoves, playerID)
	}
	if len(state.Ctx.ActivePlayers) == 0 {
		state = drainActivePlayers(state)
	}
	return state
}

// lookupStage finds a stage config by name, preferring the phase-level
// override over the global Turn.Stages map.
func lookupStage(game *Game, phase, stage string) *StageConfig {
	if phase != "" {
		if p, ok := game.Phases[phase]; ok && p.Turn != nil {
			if s, ok := p.Turn.Stages[stage]; ok {
				return s
			}
		}
	}
	if game.Turn != nil {
		if s, ok := game.Turn.Stages[stage]; ok {
			return s
		}
	}
	return nil
}

// removePlayer eliminates a player from the match — splices them out of
// PlayOrder, adjusts PlayOrderPos so the next call to advancePlayOrderPos
// lands on the right seat, and ends the turn if the eliminated player was
// the current player.
func removePlayer(game *Game, state State, playerID string, events *Events) State {
	idx := -1
	for i, p := range state.Ctx.PlayOrder {
		if p == playerID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return state
	}
	// Splice.
	state.Ctx.PlayOrder = append(state.Ctx.PlayOrder[:idx], state.Ctx.PlayOrder[idx+1:]...)

	// Adjust PlayOrderPos. If the removed seat was before or at the cursor,
	// keep the same index so the NEXT player picks up naturally — except
	// if the cursor was the removed player, in which case we end their
	// turn.
	wasCurrent := state.Ctx.CurrentPlayer == playerID
	if idx < state.Ctx.PlayOrderPos {
		state.Ctx.PlayOrderPos--
	} else if idx == state.Ctx.PlayOrderPos && !wasCurrent {
		// pos was already pointing at someone else, but the shift moved
		// the cursor; nothing to do — the cursor still resolves correctly.
	}
	if len(state.Ctx.PlayOrder) == 0 {
		state.Ctx.CurrentPlayer = ""
		state.Ctx.PlayOrderPos = 0
		return state
	}
	if state.Ctx.PlayOrderPos >= len(state.Ctx.PlayOrder) {
		state.Ctx.PlayOrderPos = 0
	}
	state.Ctx.CurrentPlayer = state.Ctx.PlayOrder[state.Ctx.PlayOrderPos]

	if wasCurrent {
		// End the turn so the next player gets a fresh turn-begin.
		state = endTurn(game, state, "", events)
	}

	// Also remove from any stage / active map.
	if state.Ctx.ActivePlayers != nil {
		delete(state.Ctx.ActivePlayers, playerID)
		if len(state.Ctx.ActivePlayers) == 0 {
			state.Ctx.ActivePlayers = nil
		}
	}
	return state
}

// applyActivePlayersFromTurn applies the Turn.ActivePlayers config (if any)
// at the start of a turn.
func applyActivePlayersFromTurn(game *Game, state State) State {
	turn := game.scopeTurn(state.Ctx.Phase)
	if turn == nil || turn.ActivePlayers == nil {
		return state
	}
	return applySetActivePlayers(state, *turn.ActivePlayers)
}

// drainActivePlayers pops the Revert stack or applies a PendingNext config
// when the current active-player set becomes empty.
func drainActivePlayers(state State) State {
	if len(state.ActiveStack) > 0 {
		top := state.ActiveStack[len(state.ActiveStack)-1]
		state.ActiveStack = state.ActiveStack[:len(state.ActiveStack)-1]
		state.Ctx.ActivePlayers = top.ActivePlayers
		state.MoveCounts = top.MoveCounts
		state.StageMinMoves = top.StageMin
		state.StageMaxMoves = top.StageMax
		return state
	}
	if state.PendingNext != nil {
		cfg := *state.PendingNext
		state.PendingNext = nil
		return applySetActivePlayers(state, cfg)
	}
	state.Ctx.ActivePlayers = nil
	state.MoveCounts = nil
	state.StageMinMoves = nil
	state.StageMaxMoves = nil
	return state
}

// applySetActivePlayers builds an ActivePlayers map from a config and
// installs it on state, with the Revert/Next bookkeeping.
func applySetActivePlayers(state State, cfg ActivePlayersConfig) State {
	if cfg.Revert {
		state.ActiveStack = append(state.ActiveStack, activeFrame{
			ActivePlayers: copyStrMap(state.Ctx.ActivePlayers),
			MoveCounts:    copyIntMap(state.MoveCounts),
			StageMin:      copyIntMap(state.StageMinMoves),
			StageMax:      copyIntMap(state.StageMaxMoves),
		})
	}
	state.PendingNext = cfg.Next

	out := map[string]string{}
	if cfg.All != nil {
		for _, pid := range state.Ctx.PlayOrder {
			out[pid] = *cfg.All
		}
	}
	if cfg.Others != nil {
		for _, pid := range state.Ctx.PlayOrder {
			if pid != state.Ctx.CurrentPlayer {
				out[pid] = *cfg.Others
			}
		}
	}
	if cfg.CurrentPlayer != nil {
		out[state.Ctx.CurrentPlayer] = *cfg.CurrentPlayer
	}
	for pid, stage := range cfg.Value {
		out[pid] = stage
	}

	state.Ctx.ActivePlayers = out
	state.MoveCounts = map[string]int{}
	state.StageMinMoves = map[string]int{}
	state.StageMaxMoves = map[string]int{}

	for pid := range out {
		if cfg.MinMoves > 0 {
			state.StageMinMoves[pid] = cfg.MinMoves
		}
		if cfg.MaxMoves > 0 {
			state.StageMaxMoves[pid] = cfg.MaxMoves
		}
		if v, ok := cfg.PerPlayerMinMoves[pid]; ok {
			state.StageMinMoves[pid] = v
		}
		if v, ok := cfg.PerPlayerMaxMoves[pid]; ok {
			state.StageMaxMoves[pid] = v
		}
	}
	if len(state.StageMinMoves) == 0 {
		state.StageMinMoves = nil
	}
	if len(state.StageMaxMoves) == 0 {
		state.StageMaxMoves = nil
	}
	if len(state.MoveCounts) == 0 {
		state.MoveCounts = nil
	}
	if len(out) == 0 {
		state.Ctx.ActivePlayers = nil
	}
	return state
}

func copyStrMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func copyIntMap(m map[string]int) map[string]int {
	if m == nil {
		return nil
	}
	out := make(map[string]int, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
