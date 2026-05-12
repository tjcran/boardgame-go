package core

// RandomPluginName is the well-known plugin key for the Random plugin. It's
// kept here (rather than in plugins/random) so core can populate
// MoveContext.Random without importing plugins/random and creating a cycle.
const RandomPluginName = "random"

// Random is the seeded PRNG exposed to moves and hooks via MoveContext.
// It's a thin wrapper around a shared uint64 state pointer so that every
// call advances the same underlying sequence, even though Random itself is
// recreated for each move.
//
// The state lives in the Random plugin's private data (see
// plugins/random); the plugin hands the engine a *Random pointing at its
// data on every move. Mutations flow back through the pointer — no Flush
// needed.
type Random struct {
	state *uint64
}

// NewRandomFromState wraps an existing state cell. Used by the Random
// plugin; not typically called by games.
func NewRandomFromState(state *uint64) *Random { return &Random{state: state} }

// State returns the current PRNG state. Useful for tests and the plugin
// when persisting or snapshotting.
func (r *Random) State() uint64 { return *r.state }

// SetState forces the PRNG state. Useful for tests / MockRandom.
func (r *Random) SetState(s uint64) { *r.state = s }

// next advances the splitmix64 PRNG. Chosen because (a) it has a tiny
// state, (b) it's deterministic, (c) the standard math/rand sources move
// across Go releases and we want byte-identical replays.
func (r *Random) next() uint64 {
	*r.state += 0x9E3779B97F4A7C15
	z := *r.state
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	z = z ^ (z >> 31)
	return z
}

// Number returns a float in [0, 1). Mirrors BGIO's `random.Number()`.
func (r *Random) Number() float64 {
	return float64(r.next()>>11) / (1 << 53)
}

// Die rolls one or more dice. Spotvalue is the die's number of sides;
// diceCount controls how many to roll. Returns a single int when
// diceCount <= 1, otherwise a []int. Mirrors BGIO's `random.Die`.
//
//	r.Die(6)         // single d6
//	r.Die(6, 3)      // three d6 as []int
//	r.Die()          // single d6 (default sides)
func (r *Random) Die(args ...int) any {
	sides := 6
	if len(args) >= 1 {
		sides = args[0]
	}
	count := 1
	if len(args) >= 2 {
		count = args[1]
	}
	if sides <= 0 {
		sides = 1
	}
	if count <= 1 {
		return r.die(sides)
	}
	out := make([]int, count)
	for i := range out {
		out[i] = r.die(sides)
	}
	return out
}

func (r *Random) die(sides int) int {
	return int(r.next()%uint64(sides)) + 1
}

// D4 rolls one or more 4-sided dice. With no args, returns a single int;
// otherwise returns a []int of length args[0].
func (r *Random) D4(count ...int) any { return r.dWrap(4, count) }

// D6 rolls one or more 6-sided dice.
func (r *Random) D6(count ...int) any { return r.dWrap(6, count) }

// D8 rolls one or more 8-sided dice.
func (r *Random) D8(count ...int) any { return r.dWrap(8, count) }

// D10 rolls one or more 10-sided dice.
func (r *Random) D10(count ...int) any { return r.dWrap(10, count) }

// D12 rolls one or more 12-sided dice.
func (r *Random) D12(count ...int) any { return r.dWrap(12, count) }

// D20 rolls one or more 20-sided dice.
func (r *Random) D20(count ...int) any { return r.dWrap(20, count) }

func (r *Random) dWrap(sides int, count []int) any {
	if len(count) == 0 {
		return r.die(sides)
	}
	return r.Die(sides, count[0])
}

// Shuffle returns a Fisher-Yates-shuffled copy of in. Free function because
// Go methods can't be generic. Mirrors BGIO's `random.Shuffle`.
//
// Use as `random.Shuffle(r, deck)`.
func Shuffle[T any](r *Random, in []T) []T {
	out := make([]T, len(in))
	copy(out, in)
	for i := len(out) - 1; i > 0; i-- {
		j := int(r.next() % uint64(i+1))
		out[i], out[j] = out[j], out[i]
	}
	return out
}
