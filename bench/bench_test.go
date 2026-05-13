// Package bench runs cross-match concurrency benchmarks. Run with:
//
//	go test -bench . -benchmem ./bench/...
//
// or with a longer duration:
//
//	go test -bench . -benchtime 5s ./bench/...
//
// The synthetic "noop" game has a single counter-incrementing move and
// no plugins / hooks. We measure throughput per concurrent match
// configuration to put a numeric floor under the JS_LIMITATIONS §1
// claim that goroutines give us free cross-match parallelism.
//
// Result interpretation:
//   - ops/sec scales near-linearly with -cpu up to the number of cores.
//   - Per-op latency is sub-microsecond on modern hardware (the reducer
//     is allocation-light).
//   - These numbers won't match a production game with rich state +
//     plugins, but they bound the engine overhead per move.
package bench

import (
	"context"
	"sync"
	"testing"

	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/match"
	"github.com/tjcran/boardgame-go/storage"
)

type counterState struct{ N int }

// noopGame is the bench fixture: 2-player game whose only move
// increments N. DisableUndo so the per-move log doesn't snapshot the
// growing history into per-move heap allocations — we want to measure
// reducer overhead, not undo bookkeeping.
func noopGame() *core.Game {
	return &core.Game{
		Name:        "noop",
		MinPlayers:  2,
		MaxPlayers:  2,
		DisableUndo: true,
		Setup:       func(_ core.Ctx, _ any) core.G { return &counterState{} },
		Moves: map[string]any{
			"inc": core.MoveFn(func(mc *core.MoveContext, _ ...any) (core.G, error) {
				return &counterState{N: mc.G.(*counterState).N + 1}, nil
			}),
		},
	}
}

// BenchmarkSingleMatchSerialMoves measures pure-engine overhead: one
// match, no concurrency, sequential Apply calls. Establishes the per-op
// floor.
func BenchmarkSingleMatchSerialMoves(b *testing.B) {
	g := noopGame()
	s := core.NewMatch(g, 0, nil)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		next, err := core.Apply(g, s, core.MoveRequest{
			PlayerID: s.Ctx.CurrentPlayer, Move: "inc",
		})
		if err != nil {
			b.Fatalf("apply: %v", err)
		}
		s = next
	}
}

// BenchmarkConcurrentMatchesViaManager spins up `concurrency` matches
// and pumps moves through each in its own goroutine via the real
// Manager. b.N total ops across all matches.
//
// This is the headline number: ops/sec measures how many move
// transactions per second the manager can process when matches run in
// parallel — what BGIO can't do behind its single Node event loop.
func BenchmarkConcurrentMatchesViaManager(b *testing.B) {
	for _, concurrency := range []int{1, 4, 16, 64} {
		b.Run(strFromInt("concurrency", concurrency), func(b *testing.B) {
			m := match.NewManager(storage.NewMemory())
			m.MustRegister(noopGame())

			type setup struct {
				id           string
				alice, bob   string
				ac, bc       string
			}
			setups := make([]setup, concurrency)
			for i := 0; i < concurrency; i++ {
				id, err := m.Create("noop", match.CreateOptions{})
				if err != nil {
					b.Fatalf("create: %v", err)
				}
				a, _ := m.Join(id, "alice", match.JoinOptions{})
				bb, _ := m.Join(id, "bob", match.JoinOptions{})
				setups[i] = setup{id, a.PlayerID, bb.PlayerID, a.PlayerCredentials, bb.PlayerCredentials}
			}

			ctx := context.Background()
			b.ResetTimer()
			b.SetParallelism(1)
			var wg sync.WaitGroup
			perMatch := (b.N + concurrency - 1) / concurrency
			for i := 0; i < concurrency; i++ {
				wg.Add(1)
				go func(s setup) {
					defer wg.Done()
					// noopGame has no MaxMoves so the turn never
					// rotates — alice (seat 0) keeps moving. That's
					// fine: we're benchmarking engine throughput, not
					// game flow.
					for j := 0; j < perMatch; j++ {
						if _, err := m.MoveReqCtx(ctx, s.id, s.alice, s.ac, core.MoveRequest{
							Move: "inc",
						}); err != nil {
							b.Errorf("move: %v", err)
							return
						}
					}
				}(setups[i])
			}
			wg.Wait()
		})
	}
}

// strFromInt avoids pulling in strconv just for sub-bench names.
func strFromInt(prefix string, n int) string {
	out := prefix + "="
	if n == 0 {
		return out + "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return out + string(buf[i:])
}
