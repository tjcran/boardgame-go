package regions

import "sort"

// ScoringKind enumerates the rule shapes the standard scope supports.
type ScoringKind int

const (
	// Plurality awards a single place: highest influence wins. Use
	// PerPlace[0] for the points and TieBreak to control multi-way
	// ties at 1st place.
	Plurality ScoringKind = iota

	// TopN awards points per place: PerPlace[0] for 1st, PerPlace[1]
	// for 2nd, etc. Players ranked beyond len(PerPlace) score 0.
	TopN

	// Threshold awards Payout points to every player with influence
	// >= Threshold. Not a competitive rule — no tie-break logic
	// applies.
	Threshold
)

// TieBreak controls what happens when two or more players tie for the
// same place under Plurality or TopN.
type TieBreak int

const (
	// Split divides the tied pot equally and rounds down. If two
	// players tie for a 3-point place, each gets 1 (one point is
	// lost to rounding). Subsequent places are skipped by the number
	// of tied players (no "ties for 1st means no 2nd place" gap).
	Split TieBreak = iota

	// NoAward gives zero to every player tied at that place;
	// subsequent places are skipped.
	NoAward

	// BothAward gives each tied player the full place's points;
	// subsequent places still receive their points.
	BothAward

	// Custom delegates to ScoringRule.CustomTie.
	Custom
)

// CustomTieFn returns each player's awarded points for one region,
// given the per-player influence map and the rule itself. Called only
// when TieBreak == Custom.
type CustomTieFn func(infl map[string]int, rule ScoringRule) map[string]int

// ScoringRule packages the rule for one Score call.
type ScoringRule struct {
	Kind ScoringKind

	// PerPlace points; index 0 is 1st place, index 1 is 2nd, etc.
	// Required for TopN. For Plurality, set PerPlace = []int{points}
	// — only the first entry is consulted.
	PerPlace []int

	// Threshold rule: minimum influence required to qualify.
	Threshold int

	// Threshold rule: points awarded to each qualifying player.
	Payout int

	// TieBreak rule applied at every place where players tie.
	TieBreak TieBreak

	// CustomTie is consulted only when TieBreak == Custom.
	CustomTie CustomTieFn
}

// ScoreRegion awards points for a single region's influence map.
// Returns playerID → points awarded by this call. Players not present
// in infl have no entry in the result.
//
// Players in the result are always present even when their score is 0,
// so callers can iterate the result to find every participant. (The
// only exception is the Empty-influence case, which returns an empty
// map.)
func ScoreRegion(infl map[string]int, rule ScoringRule) map[string]int {
	if rule.TieBreak == Custom && rule.CustomTie != nil {
		return rule.CustomTie(infl, rule)
	}
	switch rule.Kind {
	case Plurality:
		return scorePlurality(infl, rule)
	case TopN:
		return scoreTopN(infl, rule)
	case Threshold:
		return scoreThreshold(infl, rule)
	}
	return map[string]int{}
}

// rankedGroups groups players by influence value, descending. Within
// each group, players are sorted alphabetically (stable + deterministic
// across Go map iteration randomisation).
func rankedGroups(infl map[string]int) [][]string {
	byVal := map[int][]string{}
	for pid, v := range infl {
		byVal[v] = append(byVal[v], pid)
	}
	vals := make([]int, 0, len(byVal))
	for v := range byVal {
		vals = append(vals, v)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(vals)))
	out := make([][]string, 0, len(vals))
	for _, v := range vals {
		group := byVal[v]
		sort.Strings(group)
		out = append(out, group)
	}
	return out
}

func scorePlurality(infl map[string]int, rule ScoringRule) map[string]int {
	if len(infl) == 0 {
		return map[string]int{}
	}
	groups := rankedGroups(infl)
	result := map[string]int{}
	for pid := range infl {
		result[pid] = 0
	}
	if len(groups) == 0 || len(rule.PerPlace) == 0 {
		return result
	}
	pts := rule.PerPlace[0]
	top := groups[0]
	if len(top) == 1 {
		result[top[0]] = pts
		return result
	}
	// Tie at first place.
	switch rule.TieBreak {
	case Split:
		share := pts / len(top)
		for _, pid := range top {
			result[pid] = share
		}
	case NoAward:
		// already zero
	case BothAward:
		for _, pid := range top {
			result[pid] = pts
		}
	}
	return result
}

func scoreTopN(infl map[string]int, rule ScoringRule) map[string]int {
	result := map[string]int{}
	for pid := range infl {
		result[pid] = 0
	}
	if len(rule.PerPlace) == 0 {
		return result
	}
	groups := rankedGroups(infl)
	place := 0 // index into PerPlace
	for _, group := range groups {
		if place >= len(rule.PerPlace) {
			break
		}
		if len(group) == 1 {
			result[group[0]] = rule.PerPlace[place]
			place++
			continue
		}
		// Tie spanning places [place, place + len(group)).
		switch rule.TieBreak {
		case Split:
			pot := 0
			n := len(group)
			for i := 0; i < n && place+i < len(rule.PerPlace); i++ {
				pot += rule.PerPlace[place+i]
			}
			share := pot / n
			for _, pid := range group {
				result[pid] = share
			}
			place += n
		case NoAward:
			// All tied players get 0; subsequent places skipped.
			place += len(group)
		case BothAward:
			pts := rule.PerPlace[place]
			for _, pid := range group {
				result[pid] = pts
			}
			place++ // BothAward consumes only one slot; next group takes the next
		}
	}
	return result
}

func scoreThreshold(_ map[string]int, _ ScoringRule) map[string]int {
	return map[string]int{}
}
