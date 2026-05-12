package core

// This file holds the engine-internal state transition helpers used by the
// reducer: turn rotation, phase rotation, stage rotation, drainEvents,
// active-player set construction. They're internal but split out from
// reducer.go because there's a lot of them and each is independently
// testable.

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
				state = endTurn(game, state, ev.turnNext)
			case evEndPhase:
				state = endPhase(game, state, "")
			case evSetPhase:
				state = endPhase(game, state, ev.phase)
			case evEndStage:
				state = endStage(game, state, mc.PlayerID)
			case evSetStage:
				if ev.stageOpts != nil {
					state = setStageLong(state, mc.PlayerID, ev.stageOpts)
				} else {
					state = setStage(state, mc.PlayerID, ev.stage)
				}
			case evEndGame:
				state.Ctx.Gameover = ev.gameover
				state = runGameOnEnd(game, state)
			case evSetActivePlayers:
				if ev.activeCfg != nil {
					state = applySetActivePlayers(state, *ev.activeCfg)
				}
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
func endTurn(game *Game, state State, nextPlayer string) State {
	turn := game.scopeTurn(state.Ctx.Phase)

	// Turn.OnEnd before rotating.
	if turn != nil && turn.OnEnd != nil {
		mc := &MoveContext{G: state.G, Ctx: state.Ctx, Events: &Events{}}
		state.G = turn.OnEnd(mc)
		// Events queued from OnEnd are intentionally NOT drained here;
		// BGIO restricts most events inside OnEnd hooks (see events.md
		// table). What is allowed (setPhase/endPhase/endGame) is handled
		// by the caller-level event drain that wraps this turn-end.
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
		// Update PlayOrderPos if the forced player is in PlayOrder.
		for i, p := range state.Ctx.PlayOrder {
			if p == nextPlayer {
				state.Ctx.PlayOrderPos = i
				break
			}
		}
	} else {
		state = advancePlayOrderPos(game, state, turn)
		if state.Ctx.Gameover != nil {
			// advancePlayOrderPos signalled phase-end-by-order-exhaustion;
			// the phase was rotated. Nothing more to do here.
			return state
		}
	}

	state.Ctx.Turn++

	// Re-apply Turn.ActivePlayers if configured for the new turn.
	state = applyActivePlayersFromTurn(game, state)

	// Turn.OnBegin for the new turn.
	state = runTurnOnBegin(game, state)
	return state
}

// advancePlayOrderPos consults the TurnOrder for the current phase to pick
// the next PlayOrderPos. If Next returns nil, the phase ends (some orders
// signal "we're done after this round").
func advancePlayOrderPos(game *Game, state State, turn *TurnConfig) State {
	order := TurnOrderDefault
	if turn != nil && !turn.Order.IsDefault() {
		order = turn.Order
	}
	mc := &MoveContext{G: state.G, Ctx: state.Ctx}
	if order.Next == nil {
		// Defensive: a partially-specified TurnOrder falls back to
		// round-robin.
		state.Ctx.PlayOrderPos = (state.Ctx.PlayOrderPos + 1) % len(state.Ctx.PlayOrder)
	} else {
		n := order.Next(mc)
		if n == nil {
			// "End of round-robin" — end the phase.
			return endPhase(game, state, "")
		}
		state.Ctx.PlayOrderPos = *n
	}
	if len(state.Ctx.PlayOrder) > 0 {
		state.Ctx.CurrentPlayer = state.Ctx.PlayOrder[state.Ctx.PlayOrderPos]
	}
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
// the phase's static Next), runs OnBegin. nextPhase="" means "use the phase's
// own Next field; if unset, leave all phases".
func endPhase(game *Game, state State, nextPhase string) State {
	if state.Ctx.Phase == "" {
		// No phase active. SetPhase with an explicit target still works.
		if nextPhase != "" {
			state.Ctx.Phase = nextPhase
			state = runPhaseEnter(game, state)
		}
		return state
	}
	current, ok := game.Phases[state.Ctx.Phase]
	if !ok {
		return state
	}

	// End the current turn first (BGIO: "Whenever a phase ends, the
	// current player's turn is first ended automatically.").
	// We do an inline minimal turn-end (no OnBegin for next turn) because
	// the phase rotation owns the new turn setup.
	if turn := game.scopeTurn(state.Ctx.Phase); turn != nil && turn.OnEnd != nil {
		mc := &MoveContext{G: state.G, Ctx: state.Ctx, Events: &Events{}}
		state.G = turn.OnEnd(mc)
	}

	// Resolve next phase.
	target := nextPhase
	if target == "" {
		mc := &MoveContext{G: state.G, Ctx: state.Ctx}
		target = current.resolveNextPhase(mc)
	}

	// OnEnd of the phase we're leaving.
	if current.OnEnd != nil {
		mc := &MoveContext{G: state.G, Ctx: state.Ctx, Events: &Events{}}
		state.G = current.OnEnd(mc)
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
		// Left all phases — just advance to the next default-ordered turn.
		state = advancePlayOrderPos(game, state, game.Turn)
		state = applyActivePlayersFromTurn(game, state)
		state = runTurnOnBegin(game, state)
		return state
	}
	state = runPhaseEnter(game, state)
	return state
}

// runPhaseEnter runs the entry sequence for the phase named in state.Ctx.Phase.
// Used both for the starting phase (from NewMatch) and for transitions.
func runPhaseEnter(game *Game, state State) State {
	mc := &MoveContext{G: state.G, Ctx: state.Ctx, Events: &Events{}}
	state = applyTurnOrderFirst(game, state, mc)
	state = applyActivePlayersFromTurn(game, state)
	state = runPhaseOnBegin(game, state)
	state = runTurnOnBegin(game, state)
	return state
}

// runPhaseOnBegin runs the active phase's OnBegin hook, if any.
func runPhaseOnBegin(game *Game, state State) State {
	if state.Ctx.Phase == "" {
		return state
	}
	p, ok := game.Phases[state.Ctx.Phase]
	if !ok || p.OnBegin == nil {
		return state
	}
	mc := &MoveContext{G: state.G, Ctx: state.Ctx, Events: &Events{}}
	state.G = p.OnBegin(mc)
	return state
}

// runTurnOnBegin runs the active scope's Turn.OnBegin hook, if any.
func runTurnOnBegin(game *Game, state State) State {
	turn := game.scopeTurn(state.Ctx.Phase)
	if turn == nil || turn.OnBegin == nil {
		return state
	}
	mc := &MoveContext{G: state.G, Ctx: state.Ctx, Events: &Events{}}
	state.G = turn.OnBegin(mc)
	return state
}

// runGameOnEnd runs Game.OnEnd after the game ends.
func runGameOnEnd(game *Game, state State) State {
	if game.OnEnd == nil {
		return state
	}
	mc := &MoveContext{G: state.G, Ctx: state.Ctx, Events: &Events{}}
	state.G = game.OnEnd(mc)
	return state
}

// setStage moves a player into a named stage.
func setStage(state State, playerID, stage string) State {
	if state.Ctx.ActivePlayers == nil {
		state.Ctx.ActivePlayers = map[string]string{}
	}
	state.Ctx.ActivePlayers[playerID] = stage
	if state.MoveCounts != nil {
		delete(state.MoveCounts, playerID)
	}
	return state
}

// setStageLong is the long-form variant carrying per-call min/max.
func setStageLong(state State, playerID string, opts *setStageOpts) State {
	state = setStage(state, playerID, opts.stage)
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
// stage's configured Next).
func endStage(game *Game, state State, playerID string) State {
	if state.Ctx.ActivePlayers == nil {
		return state
	}
	currentStage := state.Ctx.ActivePlayers[playerID]

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
		state.Ctx.ActivePlayers[playerID] = nextStage
		if state.MoveCounts != nil {
			delete(state.MoveCounts, playerID)
		}
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

// applyActivePlayersFromTurn applies the Turn.ActivePlayers config (if any)
// at the start of a turn.
func applyActivePlayersFromTurn(game *Game, state State) State {
	turn := game.scopeTurn(state.Ctx.Phase)
	if turn == nil || turn.ActivePlayers == nil {
		return state
	}
	return applySetActivePlayers(state, *turn.ActivePlayers)
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

// copyStrMap / copyIntMap defensively duplicate maps used by the
// Revert-stack frames so future mutations of the working state don't leak
// into the snapshot.

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
