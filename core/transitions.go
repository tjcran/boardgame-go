package core

// This file holds the engine-internal state transition helpers used by the
// reducer: turn rotation, phase rotation, stage rotation, drainEvents,
// active-player set construction.
//
// All hooks share a single Events queue with their caller, so events
// queued inside a hook (e.g. phase.OnBegin calling events.EndTurn) are
// picked up by the surrounding drain loop. BGIO has a bug (#1237) where
// these are silently dropped — we explicitly fix it.

// maxChainedMoves caps Events.RunMove recursion per move. Keeps a
// misbehaving game from infinite-looping the engine.
const maxChainedMoves = 32

// hookEnv is the ambient environment engine-invoked game code runs in: the
// event queue shared with the surrounding drain loop, plus the plugin API
// table resolved for this Apply.
//
// The two travel together through every transition helper because a hook
// missing either one is silently, not loudly, broken: no queue means its
// events are dropped (BGIO #1237), and no plugin table means mc.Random is
// nil and its randomness quietly isn't random.
//
// The API objects are the *same* ones the surrounding move got, which is
// what makes hook mutations persist: plugin APIs share a state pointer
// with the persisted plugin data, and Apply flushes that data once through
// the move's MoveContext. Rebuilding the table per hook would strand
// mutations from any plugin that doesn't share a pointer.
type hookEnv struct {
	events  *Events
	plugins map[string]any
}

// newHookEnv resolves a fresh plugin API table for state. Only for entry
// points that run hooks outside a move (match setup, forced turn end);
// Apply and applyStep construct hookEnv from the table they already hold.
func newHookEnv(game *Game, state State, events *Events) *hookEnv {
	return &hookEnv{events: events, plugins: buildPluginAPIs(game, state, "")}
}

// mc builds the MoveContext for a hook that is allowed to queue events.
func (e *hookEnv) mc(state State, playerID string) *MoveContext {
	return (&MoveContext{
		G: state.G, Ctx: state.Ctx, PlayerID: playerID, Events: e.events,
	}).attachPlugins(e.plugins)
}

// mcNoEvents is mc for the TurnOrder callbacks (Next) and Phase.Next, which
// BGIO does not let queue transitions — they answer "who/what is next?",
// they don't drive the rotation. They still get the plugin table: choosing
// a random next player is a legitimate thing for a turn order to do.
func (e *hookEnv) mcNoEvents(state State) *MoveContext {
	return (&MoveContext{G: state.G, Ctx: state.Ctx}).attachPlugins(e.plugins)
}

// runChainedMove resolves and applies a move queued via Events.RunMove.
// It re-uses the surrounding events queue so further events from the
// chained move land in the same drain loop. Errors (unknown move,
// invalid args) are dropped; that's the price of "fire-and-forget"
// dispatch from a move body. Use Apply directly when you need errors.
func runChainedMove(game *Game, state State, parent *MoveContext, name string, args []any) State {
	// Resolve from the current scope: phase first, then global. Stage
	// lookups don't apply here — RunMove is dispatched in the surrounding
	// move's player context, not a stage's.
	scope := game.scopeMoves(state.Ctx.Phase)
	raw, ok := scope[name]
	if !ok {
		return state
	}
	move, err := asMove(raw)
	if err != nil {
		return state
	}
	mc := (&MoveContext{
		G:        state.G,
		Ctx:      state.Ctx,
		PlayerID: parent.PlayerID,
		Events:   parent.Events,
		Context:  parent.Context,
	}).attachPlugins(parent.Plugins)
	mc.chainedMoves = parent.chainedMoves // share counter
	nextG, err := move.Move(mc, args...)
	if err != nil {
		return state
	}
	state.G = nextG
	state.StateID++
	state.Log = append(state.Log, LogEntry{
		Kind: "chained-move", Move: name, PlayerID: parent.PlayerID,
		Args: append([]any(nil), args...), Turn: state.Ctx.Turn,
		Phase: state.Ctx.Phase,
	})
	if mc.extra != nil {
		state.Log = append(state.Log, mc.extra.entries...)
	}
	// Propagate the share-counter back so siblings see the incremented total.
	parent.chainedMoves = mc.chainedMoves
	return state
}

// drainEvents flushes the events queue in order, applying each transition.
// Some events can in turn enqueue more events (e.g. an OnEnd hook calling
// SetPhase), so this drains until the queue is empty.
func drainEvents(game *Game, state State, mc *MoveContext, env *hookEnv) (State, error) {
	for {
		batch := env.events.drain()
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
				state = endTurn(game, state, ev.turnNext, env)
			case evEndPhase:
				state = endPhase(game, state, "", env)
			case evSetPhase:
				state = endPhase(game, state, ev.phase, env)
			case evEndStage:
				state = endStage(game, state, mc.PlayerID, env)
			case evSetStage:
				if ev.stageOpts != nil {
					state = setStageLong(game, state, mc.PlayerID, ev.stageOpts, env)
				} else {
					state = setStage(game, state, mc.PlayerID, ev.stage, env)
				}
			case evEndGame:
				state.Ctx.Gameover = ev.gameover
				state = runGameOnEnd(game, state, env)
			case evSetActivePlayers:
				if ev.activeCfg != nil {
					state = applySetActivePlayers(state, *ev.activeCfg)
				}
			case evRemovePlayer:
				state = removePlayer(game, state, ev.playerID, env)
			case evRunMove:
				// Recursion bound: cap chained moves per drain so a
				// poorly-written game doesn't infinite-loop.
				mc.chainedMoves++
				if mc.chainedMoves > maxChainedMoves {
					continue
				}
				state = runChainedMove(game, state, mc, ev.moveName, ev.moveArgs)
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
// env carries the shared queue: hooks called here queue into it and the
// outer drain loop picks them up.
func endTurn(game *Game, state State, nextPlayer string, env *hookEnv) State {
	turn := game.scopeTurn(state.Ctx.Phase)

	// Turn.OnEnd before rotating.
	if turn != nil && turn.OnEnd != nil {
		mc := env.mc(state, "")
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
		state = advancePlayOrderPos(game, state, turn, env)
		if state.Ctx.Gameover != nil {
			return state
		}
	}

	state.Ctx.Turn++

	state = applyActivePlayersFromTurn(game, state)
	state = runTurnOnBegin(game, state, env)
	return state
}

// advancePlayOrderPos consults the TurnOrder for the current phase to pick
// the next PlayOrderPos. If Next returns nil, the phase ends (some orders
// signal "we're done after this round").
func advancePlayOrderPos(game *Game, state State, turn *TurnConfig, env *hookEnv) State {
	if len(state.Ctx.PlayOrder) == 0 {
		// Everyone has been eliminated. Nothing to advance to.
		return state
	}
	order := TurnOrderDefault
	if turn != nil && !turn.Order.IsDefault() {
		order = turn.Order
	}
	mc := env.mcNoEvents(state)
	if order.Next == nil {
		state.Ctx.PlayOrderPos = (state.Ctx.PlayOrderPos + 1) % len(state.Ctx.PlayOrder)
	} else {
		n := order.Next(mc)
		if n == nil {
			return endPhase(game, state, "", env)
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
func endPhase(game *Game, state State, nextPhase string, env *hookEnv) State {
	if state.Ctx.Phase == "" {
		if nextPhase != "" {
			state.Ctx.Phase = nextPhase
			state = runPhaseEnter(game, state, env)
		}
		return state
	}
	current, ok := game.Phases[state.Ctx.Phase]
	if !ok {
		return state
	}

	// Whenever a phase ends, the current player's turn is first ended.
	if turn := game.scopeTurn(state.Ctx.Phase); turn != nil && turn.OnEnd != nil {
		mc := env.mc(state, "")
		state.G = applyFnWrapHook(game, turn.OnEnd, GameMethodTurnOnEnd)(mc)
		state = flushExtraLog(state, mc)
	}

	// Resolve next phase.
	target := nextPhase
	if target == "" {
		mc := env.mcNoEvents(state)
		target = current.resolveNextPhase(mc)
	}

	// OnEnd of the phase we're leaving.
	if current.OnEnd != nil {
		mc := env.mc(state, "")
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
		state = advancePlayOrderPos(game, state, game.Turn, env)
		state = applyActivePlayersFromTurn(game, state)
		state = runTurnOnBegin(game, state, env)
		return state
	}
	state = runPhaseEnter(game, state, env)
	return state
}

// runPhaseEnter runs the entry sequence for the phase named in state.Ctx.Phase.
// Used both for the starting phase (from NewMatch) and for transitions.
func runPhaseEnter(game *Game, state State, env *hookEnv) State {
	mc := env.mc(state, "")
	state = applyTurnOrderFirst(game, state, mc)
	state = applyActivePlayersFromTurn(game, state)
	state = runPhaseOnBegin(game, state, env)
	state = runTurnOnBegin(game, state, env)
	return state
}

// runPhaseOnBegin runs the active phase's OnBegin hook, if any. Events queued
// by the hook land in env.events for the surrounding drain to pick up.
func runPhaseOnBegin(game *Game, state State, env *hookEnv) State {
	if state.Ctx.Phase == "" {
		return state
	}
	p, ok := game.Phases[state.Ctx.Phase]
	if !ok || p.OnBegin == nil {
		return state
	}
	mc := env.mc(state, "")
	state.G = applyFnWrapHook(game, p.OnBegin, GameMethodPhaseOnBegin)(mc)
	state = flushExtraLog(state, mc)
	return state
}

// runTurnOnBegin runs the active scope's Turn.OnBegin hook, if any.
func runTurnOnBegin(game *Game, state State, env *hookEnv) State {
	turn := game.scopeTurn(state.Ctx.Phase)
	if turn == nil || turn.OnBegin == nil {
		return state
	}
	mc := env.mc(state, "")
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
func runGameOnEnd(game *Game, state State, env *hookEnv) State {
	if game.OnEnd == nil {
		return state
	}
	mc := env.mc(state, "")
	state.G = applyFnWrapHook(game, game.OnEnd, GameMethodGameOnEnd)(mc)
	state = flushExtraLog(state, mc)
	return state
}

// setStage moves a player into a named stage. Fires stage.OnBegin if set.
func setStage(game *Game, state State, playerID, stageName string, env *hookEnv) State {
	if state.Ctx.ActivePlayers == nil {
		state.Ctx.ActivePlayers = map[string]string{}
	}
	state.Ctx.ActivePlayers[playerID] = stageName
	if state.MoveCounts != nil {
		delete(state.MoveCounts, playerID)
	}
	// Fire stage.OnBegin if the new stage defines one.
	if stage := lookupStage(game, state.Ctx.Phase, stageName); stage != nil && stage.OnBegin != nil {
		mc := env.mc(state, playerID)
		state.G = applyFnWrapHook(game, stage.OnBegin, GameMethodStageOnBegin)(mc)
		state = flushExtraLog(state, mc)
	}
	return state
}

// setStageLong is the long-form variant carrying per-call min/max.
func setStageLong(game *Game, state State, playerID string, opts *setStageOpts, env *hookEnv) State {
	state = setStage(game, state, playerID, opts.stage, env)
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
func endStage(game *Game, state State, playerID string, env *hookEnv) State {
	if state.Ctx.ActivePlayers == nil {
		return state
	}
	currentStage := state.Ctx.ActivePlayers[playerID]

	// Fire OnEnd before mutating state.
	if stage := lookupStage(game, state.Ctx.Phase, currentStage); stage != nil && stage.OnEnd != nil {
		mc := env.mc(state, playerID)
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
		state = setStage(game, state, playerID, nextStage, env)
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
func removePlayer(game *Game, state State, playerID string, env *hookEnv) State {
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
		state = endTurn(game, state, "", env)
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
