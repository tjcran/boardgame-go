// Package bots provides server-side bot players: a small Bot interface,
// concrete RandomBot and MCTSBot implementations, and an AutoPlayer that
// runs a bot against a live match.io.
//
// BGIO ships RandomBot and MCTSBot too, but they run in the Node.js
// engine's single thread — deep MCTS during a real player's turn
// stalls every other match on the process. Here each bot is a goroutine;
// MCTS rollouts run in parallel with players' moves on other matches,
// and they're meaningfully faster than the JS equivalent at the same
// iteration count.
package bots

import (
	"context"
	"errors"
	"math"
	"sync"

	"github.com/tjcran/boardgame-go/core"
)

// Action is one move a bot might play. The Move name and Args correspond
// 1:1 to a core.MoveRequest.
type Action struct {
	Move string
	Args []any
}

// EnumerateFn returns every legal Action for a given (G, Ctx, playerID).
// Games supply one of these to opt into bot support — the engine can't
// enumerate moves on its own (move signatures take typed args).
//
// The function should be pure (no I/O, no goroutines): bots may call it
// thousands of times per turn during MCTS rollouts.
type EnumerateFn func(g core.G, ctx core.Ctx, playerID string) []Action

// Bot is the strategy interface. Play takes a State and returns the
// Action the bot wants to play next. Implementations should respect ctx
// cancellation (long MCTS runs honour deadlines).
type Bot interface {
	Play(ctx context.Context, state core.State, playerID string) (Action, error)
}

// ErrNoMoves is returned when Enumerate yields no legal action.
var ErrNoMoves = errors.New("no legal moves available")

// RandomBot picks one of the enumerated actions uniformly at random.
// Useful as a baseline opponent.
type RandomBot struct {
	Enumerate EnumerateFn
	// Seed is the PRNG seed; passed through plugins/random's seed
	// scheme so the same value produces the same sequence.
	Seed any

	once   sync.Once
	state  uint64
	stateM sync.Mutex
}

// Play implements Bot.
func (b *RandomBot) Play(ctx context.Context, state core.State, playerID string) (Action, error) {
	actions := b.Enumerate(state.G, state.Ctx, playerID)
	if len(actions) == 0 {
		return Action{}, ErrNoMoves
	}
	b.once.Do(func() { b.state = seedToUint64(b.Seed) })
	b.stateM.Lock()
	defer b.stateM.Unlock()
	idx := int(next(&b.state)%uint64(len(actions)))
	return actions[idx], nil
}

// MCTSBot runs Monte Carlo tree search at the root. For each iteration it
// (1) picks an action by UCT, (2) applies it, (3) random-rolls out to a
// terminal state, and (4) backpropagates the win/draw/loss. Picks the
// most-visited root action at the end.
//
// Game must be the same game that produced state — we use it to call
// core.Apply during rollouts. Iterations defaults to 100; higher values
// give better play at the cost of more wall-clock time.
type MCTSBot struct {
	Game       *core.Game
	Enumerate  EnumerateFn
	Iterations int
	// Seed seeds the rollout PRNG; deterministic for tests/replay.
	Seed any
}

// Play implements Bot.
func (b *MCTSBot) Play(ctx context.Context, state core.State, playerID string) (Action, error) {
	if b.Game == nil || b.Enumerate == nil {
		return Action{}, errors.New("MCTSBot: Game and Enumerate are required")
	}
	actions := b.Enumerate(state.G, state.Ctx, playerID)
	if len(actions) == 0 {
		return Action{}, ErrNoMoves
	}
	iters := b.Iterations
	if iters == 0 {
		iters = 100
	}
	rngState := seedToUint64(b.Seed)

	wins := make([]float64, len(actions))
	visits := make([]int, len(actions))

	for it := 0; it < iters; it++ {
		// Cooperative cancellation: stop early if the caller's context is done.
		if ctx != nil {
			select {
			case <-ctx.Done():
				it = iters
				continue
			default:
			}
		}
		idx := selectByUCT(wins, visits, it)
		afterMove, err := core.Apply(b.Game, state, core.MoveRequest{
			PlayerID: playerID, Move: actions[idx].Move, Args: actions[idx].Args,
		})
		if err != nil {
			visits[idx]++
			continue
		}
		result := b.rollout(afterMove, playerID, &rngState)
		wins[idx] += result
		visits[idx]++
	}

	bestIdx, bestVisits := 0, -1
	for i, v := range visits {
		if v > bestVisits {
			bestVisits = v
			bestIdx = i
		}
	}
	return actions[bestIdx], nil
}

// rollout plays random moves until terminal (or depth cap). Returns the
// caller's win fraction: 1.0 win, 0.5 draw, 0.0 loss.
func (b *MCTSBot) rollout(state core.State, viewer string, rng *uint64) float64 {
	const maxDepth = 100
	for d := 0; d < maxDepth && state.Ctx.Gameover == nil; d++ {
		actions := b.Enumerate(state.G, state.Ctx, state.Ctx.CurrentPlayer)
		if len(actions) == 0 {
			break
		}
		a := actions[int(next(rng)%uint64(len(actions)))]
		nx, err := core.Apply(b.Game, state, core.MoveRequest{
			PlayerID: state.Ctx.CurrentPlayer, Move: a.Move, Args: a.Args,
		})
		if err != nil {
			break
		}
		state = nx
	}
	return score(state.Ctx.Gameover, viewer)
}

// score interprets a Gameover value in the conventional BGIO shape:
// {"winner": "X"} → 1 if X==viewer else 0; {"draw": true} → 0.5; nil or
// other shapes → 0.5 (treat unfinished/unknown as draw, neutral for
// MCTS).
func score(gameover any, viewer string) float64 {
	if gameover == nil {
		return 0.5
	}
	m, ok := gameover.(map[string]any)
	if !ok {
		return 0.5
	}
	if w, ok := m["winner"].(string); ok {
		if w == viewer {
			return 1.0
		}
		return 0.0
	}
	if b, _ := m["draw"].(bool); b {
		return 0.5
	}
	return 0.5
}

// selectByUCT returns the index of the best child by the UCB1 formula.
// For unvisited children, returns the first one in order so every action
// is explored at least once.
func selectByUCT(wins []float64, visits []int, total int) int {
	for i, v := range visits {
		if v == 0 {
			return i
		}
	}
	bestIdx := 0
	bestScore := -1.0
	tlog := math.Log(float64(total + 1))
	for i := range wins {
		score := wins[i]/float64(visits[i]) +
			math.Sqrt(2.0*tlog/float64(visits[i]))
		if score > bestScore {
			bestScore = score
			bestIdx = i
		}
	}
	return bestIdx
}

// --- shared PRNG helpers ---------------------------------------------------
// Same splitmix64 as core.Random so bot rollouts use the same PRNG family.

func next(state *uint64) uint64 {
	*state += 0x9E3779B97F4A7C15
	z := *state
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	return z ^ (z >> 31)
}

func seedToUint64(seed any) uint64 {
	switch s := seed.(type) {
	case nil:
		return 0xDEADBEEFCAFEBABE
	case uint64:
		return s
	case int:
		return uint64(s)
	case int64:
		return uint64(s)
	case string:
		var h uint64 = 14695981039346656037 // FNV-1a offset basis
		for i := 0; i < len(s); i++ {
			h ^= uint64(s[i])
			h *= 1099511628211
		}
		return h
	default:
		return 0
	}
}
