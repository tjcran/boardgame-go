package testhelpers

import "github.com/tjcran/boardgame-go/core"

// MockRandom is a drop-in replacement for the Random plugin that lets a
// test override specific methods (D6, Die, Number, Shuffle). Anything not
// overridden falls back to a deterministic seeded source so the test still
// runs.
//
// Mirrors BGIO's `MockRandom` from `boardgame.io/testing`.
//
// Usage:
//
//	g.Plugins = []core.Plugin{
//	    testhelpers.MockRandom{
//	        D6Fn: func(count ...int) any { return 6 },
//	    },
//	}
//
// The plugin keeps the same key (core.RandomPluginName) as the real Random
// plugin, so `mc.Random.D6()` works in the move under test exactly as it
// would in production.
type MockRandom struct {
	// D6Fn, when non-nil, replaces *core.Random.D6.
	D6Fn func(count ...int) any
	// DieFn replaces *core.Random.Die.
	DieFn func(args ...int) any
	// NumberFn replaces *core.Random.Number.
	NumberFn func() float64
	// ShuffleFn replaces *core.Random.Shuffle. Receives elements as []any.
	ShuffleFn func(in []any) []any
}

// Name implements core.Plugin and matches the real Random plugin's key so
// this overrides it by registration order.
func (m MockRandom) Name() string { return core.RandomPluginName }

// Setup pre-seeds a real PRNG state so any un-mocked methods still return
// deterministic values.
func (m MockRandom) Setup(_ core.G, _ core.Ctx, _ *core.Game) any {
	s := uint64(0xC0FFEE)
	return &mockData{S: &s}
}

// API constructs a *core.Random with the configured override callbacks.
// Because we return the canonical type, `mc.Random` keeps working
// everywhere — including inside the engine's per-move plumbing.
func (m MockRandom) API(raw any, _ core.G, _ core.Ctx, _ string, _ *core.Game) any {
	d := raw.(*mockData)
	r := core.NewRandomFromState(d.S)
	r.MockD6 = m.D6Fn
	r.MockDie = m.DieFn
	r.MockNumber = m.NumberFn
	r.MockShuffle = m.ShuffleFn
	return r
}

// Flush is a no-op: the API mutates the shared state pointer.
func (m MockRandom) Flush(d, _ any, _ core.G, _ core.Ctx, _ *core.Game) any { return d }

// PlayerView hides the PRNG state from clients, matching the real plugin.
func (m MockRandom) PlayerView(_ any, _ core.G, _ core.Ctx, _ string, _ *core.Game) any {
	return nil
}

type mockData struct {
	S *uint64
}
