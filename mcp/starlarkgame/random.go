package starlarkgame

import (
	"fmt"

	"github.com/tjcran/boardgame-go/core"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

// bridgeRandom wraps core.Random (the engine's splitmix64 PRNG) and exposes
// range/shuffle/choice to Starlark. Using core.Random directly keeps designed
// games reproducible under the same seed-management discipline as built-in
// games.
//
// Note: plugins/random exposes a Plugin (game-engine lifecycle object), not a
// bare PRNG. The actual PRNG lives in core.Random; we create one here via
// core.NewRandomFromState with a locally owned uint64 state.
type bridgeRandom struct {
	state uint64
	rng   *core.Random
}

// AttachSeededRandom installs a fresh core.Random seeded with the given value.
// Subsequent ctx.random calls in the spec draw from this PRNG.
//
// The seed is passed as uint64 to core.NewRandomFromState's state pointer;
// the splitmix64 algorithm in core.Random advances it on every draw.
func (c *BridgeCtx) AttachSeededRandom(seed uint64) {
	br := &bridgeRandom{state: seed}
	br.rng = core.NewRandomFromState(&br.state)
	c.rng = br
}

// asStarlark returns a frozen starlarkstruct exposing three builtins:
//
//	ctx.random.range(n)       → int in [0, n)
//	ctx.random.shuffle(list)  → None (in-place Fisher-Yates)
//	ctx.random.choice(list)   → element at random index
func (r *bridgeRandom) asStarlark() starlark.Value {
	return starlarkstruct.FromStringDict(starlark.String("random"), starlark.StringDict{
		"range":   starlark.NewBuiltin("range", r.rangeBuiltin),
		"shuffle": starlark.NewBuiltin("shuffle", r.shuffleBuiltin),
		"choice":  starlark.NewBuiltin("choice", r.choiceBuiltin),
	})
}

// intn returns a uniform int in [0, n) using core.Random.Number() (which
// returns float64 in [0,1)). This mirrors the same splitmix64 sequence used
// by all built-in games, ensuring replay determinism.
func (r *bridgeRandom) intn(n int) int {
	return int(r.rng.Number() * float64(n))
}

func (r *bridgeRandom) rangeBuiltin(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("random.range: expected 1 arg, got %d", len(args))
	}
	n, ok := args[0].(starlark.Int)
	if !ok {
		return nil, fmt.Errorf("random.range: arg must be int, got %s", args[0].Type())
	}
	ni, _ := n.Int64()
	if ni <= 0 {
		return nil, fmt.Errorf("random.range: n must be > 0, got %d", ni)
	}
	return starlark.MakeInt(r.intn(int(ni))), nil
}

func (r *bridgeRandom) shuffleBuiltin(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("random.shuffle: expected 1 arg, got %d", len(args))
	}
	lst, ok := args[0].(*starlark.List)
	if !ok {
		return nil, fmt.Errorf("random.shuffle: arg must be list, got %s", args[0].Type())
	}
	n := lst.Len()
	for i := n - 1; i > 0; i-- {
		j := r.intn(i + 1)
		bi, bj := lst.Index(i), lst.Index(j)
		if err := lst.SetIndex(i, bj); err != nil {
			return nil, err
		}
		if err := lst.SetIndex(j, bi); err != nil {
			return nil, err
		}
	}
	return starlark.None, nil
}

func (r *bridgeRandom) choiceBuiltin(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("random.choice: expected 1 arg, got %d", len(args))
	}
	lst, ok := args[0].(*starlark.List)
	if !ok {
		return nil, fmt.Errorf("random.choice: arg must be list, got %s", args[0].Type())
	}
	if lst.Len() == 0 {
		return nil, fmt.Errorf("random.choice: empty list")
	}
	return lst.Index(r.intn(lst.Len())), nil
}
