// Package shop provides a refreshable market for auto-battler and
// deckbuilder games built on boardgame-go.
//
// A Shop is two ccg.Zones — Slots (the visible row) and Stock (the
// supply the row refills from) — plus a Size (the row's target
// count). Operations:
//
//   - Clear / Fill / Roll: discard un-frozen Slot items (or move them
//     to a discard zone), then draw fresh from Stock up to Size.
//   - Freeze / Unfreeze / IsFrozen: mark a Slot entry to survive the
//     next Roll. Frozen state lives in Entity.Attrs[ShopFrozenAttrKey].
//   - Buy: move an item from Slots into a destination zone (typically
//     the player's hand or bench). Buy does NOT check cost — combine
//     with economy.Pool.Spend in the move handler, and roll back on
//     ErrInsufficient.
//
// Shop holds no state; pass *ccg.State to its methods. The library is
// opt-in: the engine never imports it.
//
// Minimal sketch (composing with economy):
//
//	market := shop.Shop{Slots: "market", Stock: "stock", Size: 5}
//	market.Roll(s, mc.Random, "")       // discards un-frozen, fills
//	market.Freeze(s, card)              // keep across the next Roll
//	if err := gold.Spend(s, cost); err == nil {
//	    market.Buy(s, card, "hand")
//	}
package shop
