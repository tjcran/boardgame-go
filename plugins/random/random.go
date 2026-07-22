// Package random is the boardgame-go Random plugin. Register it on a
// Game.Plugins to give moves access to a deterministic, seeded PRNG via
// mc.Random (or mc.Plugins["random"].(*core.Random)).
//
// The PRNG state is stored in the match's plugin private area; the engine
// strips it before pushing state to clients so the future random sequence
// cannot be predicted from outside the server.
//
// Mirrors boardgame.io's built-in Random plugin (boardgame.io/plugins).
package random

import (
	"encoding/binary"
	"encoding/json"
	"hash/fnv"
	"log/slog"
	"math"
	"sync"

	"github.com/tjcran/boardgame-go/core"
)

// PluginName is the well-known key used to look the plugin up in
// MoveContext.Plugins and State.Plugins. Equals core.RandomPluginName.
const PluginName = core.RandomPluginName

// unseededFallback is the PRNG state used when no seed source supplied
// entropy. It is a constant, so every match that reaches it shares one
// stream — see resolveSeed, which warns when that happens.
const unseededFallback = 0xDEADBEEFCAFEBABE

// Plugin is the plugin instance. Construct with New(seed).
type Plugin struct {
	seed uint64

	// warnedUnseeded keeps the "no entropy anywhere" warning to one line
	// per registered plugin instead of one per match created.
	warnedUnseeded sync.Once
}

// New returns a Random plugin. seed can be string, int, int64, uint64, or
// nil ("no explicit seed" — the match seed is used instead; see
// resolveSeed for the full precedence). String seeds are FNV-hashed.
func New(seed any) *Plugin {
	return &Plugin{seed: seedToUint64(seed)}
}

// state is the persisted private payload. It's a pointer-receiver type so
// the engine can mutate via the *core.Random shared pointer.
type state struct {
	S uint64 `json:"s"` // current PRNG state
}

// Name implements core.Plugin.
func (p *Plugin) Name() string { return PluginName }

// Setup initialises the PRNG for a new match. See resolveSeed for where
// the starting state comes from.
func (p *Plugin) Setup(_ core.G, ctx core.Ctx, game *core.Game) any {
	return &state{S: p.resolveSeed(ctx, game)}
}

// resolveSeed picks the PRNG's starting state, most-specific source first:
//
//  1. random.New(seed) — the caller pinned a stream on purpose (tests,
//     replay harnesses, reproducible benchmarks).
//  2. Game.Seed — the game definition pinned one; Game.Seed documents
//     itself as the override for the engine's per-match seed.
//  3. Ctx.Seed — the per-match secret entropy assigned at creation time
//     (match.Manager generates it, NewMatchSeeded plumbs it). This is the
//     production path, and the reason ReplaySeeded reproduces a match: it
//     rebuilds from NewMatchSeeded with the same value, so the stream that
//     derives from it is the same stream.
//  4. A constant, last resort. Only reachable when nothing above supplied
//     entropy — an unseeded core.NewMatch plus random.New(nil) — which
//     means every match on the process really does replay one identical
//     stream. Warn rather than be silently deterministic.
func (p *Plugin) resolveSeed(ctx core.Ctx, game *core.Game) uint64 {
	if p.seed != 0 {
		return p.seed
	}
	if game != nil && game.Seed != nil {
		if s := seedToUint64(game.Seed); s != 0 {
			return s
		}
	}
	if ctx.Seed != 0 {
		return ctx.Seed
	}
	p.warnedUnseeded.Do(func() {
		slog.Warn("random plugin has no seed source: plugin seed, Game.Seed and Ctx.Seed are all zero, so every match will replay one identical PRNG stream. Create matches via match.Manager or core.NewMatchSeeded, or pass random.New(seed) for deliberate determinism.",
			"plugin", PluginName)
	})
	return unseededFallback
}

// Decode implements core.PluginDecode: after a persistence round-trip
// the manager hands back the raw JSON so the PRNG state rehydrates into
// its typed form with exact uint64 precision (a float64 round-trip via
// map[string]any would corrupt states above 2^53).
func (p *Plugin) Decode(raw json.RawMessage) (any, error) {
	var s state
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// API hands the move a *core.Random whose internal state pointer aims at
// the plugin's private data. Mutations from D6/Shuffle/etc. write straight
// through to the persisted state.
//
// data is *state on the happy path. A match reloaded WITHOUT the
// manager's PluginDecode pass (older callers, hand-rolled loaders)
// arrives as generic JSON — rather than panicking mid-serve and
// bricking the match, degrade to a best-effort rebuild.
func (p *Plugin) API(data any, _ core.G, ctx core.Ctx, _ string, game *core.Game) any {
	switch d := data.(type) {
	case *state:
		return core.NewRandomFromState(&d.S)
	case map[string]any:
		if f, ok := d["s"].(float64); ok {
			s := &state{S: uint64(f)}
			return core.NewRandomFromState(&s.S)
		}
	}
	// Unknown shape — a fresh state from the same sources Setup uses keeps
	// the match serviceable (future draws remain random, just not the
	// original sequence).
	s := &state{S: p.resolveSeed(ctx, game)}
	return core.NewRandomFromState(&s.S)
}

// Flush is a no-op because the API and data share a state pointer; any
// mutation has already landed in data.
func (p *Plugin) Flush(data, _ any, _ core.G, _ core.Ctx, _ *core.Game) any {
	return data
}

// PlayerView hides the PRNG state from clients. If the state leaked, a
// client could predict the next dice roll / shuffle.
func (p *Plugin) PlayerView(_ any, _ core.G, _ core.Ctx, _ string, _ *core.Game) any {
	return nil
}

// seedToUint64 converts a BGIO-style seed (string or number) into our
// PRNG's uint64 state. Strings are FNV-1a hashed. Numbers cast directly.
func seedToUint64(seed any) uint64 {
	switch s := seed.(type) {
	case nil:
		return 0
	case uint64:
		return s
	case int:
		return uint64(s)
	case int64:
		return uint64(s)
	case int32:
		return uint64(s)
	case float64:
		// Persist the bit pattern so e.g. 42 and 42.0 produce the same
		// stream (uint64 cast preserves integer bits for in-range values).
		if s == float64(uint64(s)) {
			return uint64(s)
		}
		var buf [8]byte
		binary.LittleEndian.PutUint64(buf[:], math.Float64bits(s))
		h := fnv.New64a()
		h.Write(buf[:])
		return h.Sum64()
	case string:
		h := fnv.New64a()
		h.Write([]byte(s))
		return h.Sum64()
	default:
		return 0
	}
}
