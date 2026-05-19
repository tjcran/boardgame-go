// Package economy provides per-turn resource pools (gold, actions,
// buys) for deckbuilder-shape and auto-battler-shape games built on
// boardgame-go.
//
// A Pool is a thin config struct over ccg.Counters — the underlying
// storage is just a named counter on a player entity. economy adds:
//
//   - Cap enforcement: Gain and Set clamp to the configured maximum
//   - Spend with ErrInsufficient — currency semantics where ccg's
//     "clamps at 0" silent floor would be a bug
//   - Scaled, a helper for the common "base + per-turn growth capped"
//     income rule
//
// Pool doesn't allocate state — pass a *ccg.State pointer to its
// methods, same shape as ccg.DeckPile. The library is opt-in: the
// engine never imports it; games drop a Pool config alongside their
// other state.
//
// Minimal sketch:
//
//	gold := economy.Pool{Owner: playerID, Kind: "gold", Cap: 10}
//	gold.Set(s, economy.Scaled(turn, 4, 1, 10))  // turn-start refresh
//	if err := gold.Spend(s, 3); err != nil {
//	    return g, err  // ErrInsufficient
//	}
//
// Composes with ccg.Counters events: every Gain/Spend/Set publishes a
// counter_changed event under the hood, so triggered abilities that
// listen for "gold gained" or "gold spent" Just Work.
package economy
