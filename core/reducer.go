package core

import (
	"errors"
	"fmt"
)

// MoveRequest names a move to apply and supplies its arguments + the player
// claiming to make it.
type MoveRequest struct {
	PlayerID string `json:"playerID"`
	Move     string `json:"move"`
	Args     []any  `json:"args"`
}

// Public sentinel errors. They're surfaced through the transport with
// matching HTTP statuses.
var (
	ErrInvalidMove   = errors.New("invalid move")
	ErrWrongPlayer   = errors.New("not your turn")
	ErrUnknownMove   = errors.New("unknown move")
	ErrGameOver      = errors.New("game is over")
	ErrMinMoves      = errors.New("minimum moves not reached")
	ErrInactivePlayer = errors.New("player is not active")
)

// Apply runs a move through the full reducer pipeline:
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
func Apply(game *Game, state State, req MoveRequest) (State, error) {
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
	mc := &MoveContext{
		G:        state.G,
		Ctx:      state.Ctx,
		PlayerID: req.PlayerID,
		Events:   events,
	}

	nextG, err := move.Move(mc, req.Args...)
	if err != nil {
		return state, err
	}

	next := state
	next.G = nextG
	mc.G = next.G // hooks see the post-move G

	// Run turn.OnMove with the updated G.
	if turn := game.scopeTurn(next.Ctx.Phase); turn != nil && turn.OnMove != nil {
		next.G = turn.OnMove(mc)
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
		mc2 := &MoveContext{G: next.G, Ctx: next.Ctx, PlayerID: req.PlayerID, Events: &Events{}}
		if res := game.EndIf(mc2); res != nil {
			next.Ctx.Gameover = res
			next = runGameOnEnd(game, next)
		}
	}

	if next.Ctx.Gameover == nil {
		next = checkPhaseEndIf(game, next)
	}

	if next.Ctx.Gameover == nil {
		next = autoEndStagesByMaxMoves(next)
	}

	if next.Ctx.Gameover == nil {
		next = checkTurnAutoEnd(game, next, move)
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
// if either fires.
func checkTurnAutoEnd(game *Game, state State, move Move) State {
	turn := game.scopeTurn(state.Ctx.Phase)
	if turn == nil {
		return state
	}
	if turn.EndIf != nil {
		mc := &MoveContext{G: state.G, Ctx: state.Ctx, Events: &Events{}}
		if end, next := turn.EndIf(mc); end {
			return endTurn(game, state, next)
		}
	}
	// MaxMoves auto-end only counts toward the turn-level limit; per-stage
	// MaxMoves is handled separately.
	if turn.MaxMoves > 0 && state.Ctx.NumMoves >= turn.MaxMoves && !move.NoLimit {
		return endTurn(game, state, "")
	}
	return state
}

// checkPhaseEndIf evaluates phase.EndIf for the current phase and rotates
// phases if it fires.
func checkPhaseEndIf(game *Game, state State) State {
	if state.Ctx.Phase == "" {
		return state
	}
	p, ok := game.Phases[state.Ctx.Phase]
	if !ok || p.EndIf == nil {
		return state
	}
	mc := &MoveContext{G: state.G, Ctx: state.Ctx, Events: &Events{}}
	end, next := p.EndIf(mc)
	if !end {
		return state
	}
	return endPhase(game, state, next)
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
