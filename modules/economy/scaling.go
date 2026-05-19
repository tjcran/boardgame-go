package economy

// Scaled returns the per-turn income for a "base + (turn-1)*per,
// capped at max" rule — the most common economy curve for both
// deckbuilders ("draw 5 cards every turn") and auto-battlers ("gold
// increases by 1 per round, capped at 10").
//
// turn is 1-indexed in the typical case; turn=0 returns base-per
// (useful when a game considers the pre-game shop tier 0). Negative
// results are clamped to 0. max == 0 means uncapped.
//
// Designed to be passed directly into Pool.Set at turn start:
//
//	gold.Set(s, economy.Scaled(turn, 4, 1, 10))
func Scaled(turn, base, per, max int) int {
	v := base + (turn-1)*per
	if v < 0 {
		v = 0
	}
	if max > 0 && v > max {
		v = max
	}
	return v
}
