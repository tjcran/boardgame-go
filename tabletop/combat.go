package tabletop

import "github.com/tjcran/boardgame-go/core"

// Pool describes a dice pool to roll: N dice of S sides. Sides defaults
// to 6 if zero; Dice ≤ 0 produces an empty roll.
type Pool struct {
	Dice  int
	Sides int
}

// Roll rolls the pool with rng and returns the rolls as a []int.
// Deterministic for a deterministic *core.Random. Empty Dice returns
// an empty (non-nil) slice so callers can range without nil checks.
func (p Pool) Roll(rng *core.Random) []int {
	if p.Dice <= 0 {
		return []int{}
	}
	sides := p.Sides
	if sides <= 0 {
		sides = 6
	}
	out := make([]int, p.Dice)
	for i := range out {
		out[i] = rng.Die(sides).(int)
	}
	return out
}

// Successes counts rolls that meet or exceed target — the standard
// "to-hit on 4+" / "to-wound on 5+" check used in dice-pool wargames.
func Successes(rolls []int, target int) int {
	n := 0
	for _, r := range rolls {
		if r >= target {
			n++
		}
	}
	return n
}

// RerollBelow returns a new slice in which every die strictly below
// `threshold` has been rerolled once. Dice at or above threshold are
// copied unchanged. The input slice is not mutated.
//
// This implements the common "reroll 1s" / "reroll wounds of 2-" rule.
// inferSides peeks at the max value in the input as a heuristic for
// die size; callers using non-d6 pools should re-roll via Pool.Roll
// directly rather than relying on this helper.
func RerollBelow(rolls []int, threshold int, rng *core.Random) []int {
	sides := inferSides(rolls)
	out := make([]int, len(rolls))
	for i, v := range rolls {
		if v < threshold {
			out[i] = rng.Die(sides).(int)
		} else {
			out[i] = v
		}
	}
	return out
}

// inferSides peeks at the max value seen in rolls; falls back to 6.
// Good enough for d6-dominant wargames; specify Pool.Sides explicitly
// for non-d6 pools.
func inferSides(rolls []int) int {
	max := 6
	for _, v := range rolls {
		if v > max {
			max = v
		}
	}
	return max
}
