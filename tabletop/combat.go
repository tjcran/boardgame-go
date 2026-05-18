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

// Resolve is the 40k-shaped hit→wound→save chain. Each stage rolls a
// d6 pool and counts successes (rolls ≥ the stage's target). Stages
// feed into each other: number of hits becomes the wound-pool size,
// number of wounds becomes the save-pool size. The save pool is the
// DEFENDER's roll: hits land when the save FAILS, so Unsaved =
// Wounds − Successes(saveRolls).
//
// HitOn / WoundOn / SaveOn are the "rolls on a d6 that succeed" — e.g.
// HitOn=3 means a d6 of 3, 4, 5, or 6 hits. Set a stage's target to 7
// or higher to make it impossible (no d6 face succeeds), and to 1 to
// make it automatic.
type Resolve struct {
	Attacks int
	HitOn   int
	WoundOn int
	SaveOn  int
}

// ResolveResult is what Resolve.Run returns.
type ResolveResult struct {
	Hits    int
	Wounds  int
	Unsaved int
}

// Run rolls the full chain with rng. Deterministic given a
// deterministic *core.Random.
func (r Resolve) Run(rng *core.Random) ResolveResult {
	if r.Attacks <= 0 {
		return ResolveResult{}
	}
	hits := Successes(Pool{Dice: r.Attacks, Sides: 6}.Roll(rng), r.HitOn)
	if hits == 0 {
		return ResolveResult{}
	}
	wounds := Successes(Pool{Dice: hits, Sides: 6}.Roll(rng), r.WoundOn)
	if wounds == 0 {
		return ResolveResult{Hits: hits}
	}
	saves := Successes(Pool{Dice: wounds, Sides: 6}.Roll(rng), r.SaveOn)
	unsaved := wounds - saves
	if unsaved < 0 {
		unsaved = 0
	}
	return ResolveResult{Hits: hits, Wounds: wounds, Unsaved: unsaved}
}
