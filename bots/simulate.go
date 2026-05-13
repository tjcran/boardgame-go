package bots

import (
	"context"
	"errors"

	"github.com/tjcran/boardgame-go/core"
)

// SimulationResult is the aggregate over Simulate's runs. Wins is
// indexed by playerID; Draws + Errors capture the remainder.
//
// Useful for: AI training corpora, balance analysis (does seat order
// confer an advantage?), regression testing after a Game change.
//
// Addresses BGIO issues #494 and #439 ("Enable collection of more game
// data when simulating" and "headless monte carlo for starting player
// advantage analysis").
type SimulationResult struct {
	// Matches is the total number of completed simulations.
	Matches int
	// Wins maps playerID → number of matches that ended with
	// ctx.Gameover == map{"winner": playerID}.
	Wins map[string]int
	// Draws is the number of matches that ended with
	// ctx.Gameover == map{"draw": true}.
	Draws int
	// Errors is the number of matches that errored out before
	// reaching ctx.Gameover (illegal moves, etc.).
	Errors int
	// AvgMoves is the mean number of move steps per completed match.
	AvgMoves float64
	// AvgTurns is the mean ctx.Turn at terminal across completed
	// matches.
	AvgTurns float64
}

// SimulateOptions bundles the per-run config for Simulate.
type SimulateOptions struct {
	// Game is required.
	Game *core.Game
	// Bots maps playerID → Bot. Must include every seat the game
	// expects; the Simulate driver replaces moves with Bot.Play
	// outputs for each seat in turn.
	Bots map[string]Bot
	// Matches is the number of games to play. Default 1.
	Matches int
	// MaxMoves caps any single match's move count to avoid infinite
	// rollouts when a buggy bot or game never terminates. Default
	// 1000 — well past anything sane.
	MaxMoves int
	// SetupData is passed through to core.NewMatch for every run.
	SetupData any
	// NumPlayers overrides the game's default; useful for variable-
	// player-count games.
	NumPlayers int
}

// Simulate runs Matches games of opts.Game between the supplied Bots.
// Returns aggregate statistics. Pure-functional: no Manager,
// storage, or network I/O — the engine runs in-memory only. Use this
// for AI training corpora, balance studies, regression runs.
func Simulate(ctx context.Context, opts SimulateOptions) (SimulationResult, error) {
	if opts.Game == nil {
		return SimulationResult{}, errors.New("Simulate: Game is required")
	}
	if len(opts.Bots) == 0 {
		return SimulationResult{}, errors.New("Simulate: at least one Bot is required")
	}
	matches := opts.Matches
	if matches <= 0 {
		matches = 1
	}
	maxMoves := opts.MaxMoves
	if maxMoves <= 0 {
		maxMoves = 1000
	}

	result := SimulationResult{
		Wins: map[string]int{},
	}
	var totalMoves, totalTurns int
	completed := 0

	for i := 0; i < matches; i++ {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		default:
		}

		state := core.NewMatch(opts.Game, opts.NumPlayers, opts.SetupData)
		moves := 0
		errored := false
		for moves < maxMoves && state.Ctx.Gameover == nil {
			pid := state.Ctx.CurrentPlayer
			// Honour active-players: pick the first active seat.
			if state.Ctx.ActivePlayers != nil {
				for k := range state.Ctx.ActivePlayers {
					pid = k
					break
				}
			}
			bot, ok := opts.Bots[pid]
			if !ok {
				errored = true
				break
			}
			action, err := bot.Play(ctx, state, pid)
			if err != nil {
				errored = true
				break
			}
			next, err := core.Apply(opts.Game, state, core.MoveRequest{
				PlayerID: pid, Move: action.Move, Args: action.Args,
			})
			if err != nil {
				errored = true
				break
			}
			state = next
			moves++
		}
		result.Matches++
		if errored || state.Ctx.Gameover == nil {
			result.Errors++
			continue
		}
		completed++
		totalMoves += moves
		totalTurns += state.Ctx.Turn
		if m, ok := state.Ctx.Gameover.(map[string]any); ok {
			if w, ok := m["winner"].(string); ok {
				result.Wins[w]++
			} else if d, _ := m["draw"].(bool); d {
				result.Draws++
			}
		}
	}
	if completed > 0 {
		result.AvgMoves = float64(totalMoves) / float64(completed)
		result.AvgTurns = float64(totalTurns) / float64(completed)
	}
	return result, nil
}
