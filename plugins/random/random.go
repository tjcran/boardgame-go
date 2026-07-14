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
	"math"

	"github.com/tjcran/boardgame-go/core"
)

// PluginName is the well-known key used to look the plugin up in
// MoveContext.Plugins and State.Plugins. Equals core.RandomPluginName.
const PluginName = core.RandomPluginName

// Plugin is the plugin instance. Construct with New(seed).
type Plugin struct {
	seed uint64
}

// New returns a Random plugin. seed can be string, int, int64, uint64, or
// nil (use Game.Seed instead). String seeds are FNV-hashed.
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

// Setup initialises the PRNG with this plugin's seed, falling back to
// Game.Seed if the plugin wasn't given one explicitly.
func (p *Plugin) Setup(_ core.G, _ core.Ctx, game *core.Game) any {
	s := p.seed
	if s == 0 && game.Seed != nil {
		s = seedToUint64(game.Seed)
	}
	if s == 0 {
		// No seed → still deterministic, just from a constant. A real-world
		// caller without a seed should pass `time.Now().UnixNano()` or
		// similar from outside the engine.
		s = 0xDEADBEEFCAFEBABE
	}
	return &state{S: s}
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
func (p *Plugin) API(data any, _ core.G, _ core.Ctx, _ string, _ *core.Game) any {
	switch d := data.(type) {
	case *state:
		return core.NewRandomFromState(&d.S)
	case map[string]any:
		if f, ok := d["s"].(float64); ok {
			s := &state{S: uint64(f)}
			return core.NewRandomFromState(&s.S)
		}
	}
	// Unknown shape — fresh state from the plugin seed keeps the match
	// serviceable (future draws remain random, just not the original
	// sequence).
	s := &state{S: p.seed}
	if s.S == 0 {
		s.S = 0xDEADBEEFCAFEBABE
	}
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
