// Package ccg provides bookkeeping primitives for CCG / TCG /
// deckbuilder / engine-builder games built on boardgame-go.
//
// Concerns:
//   - Entities with stable IDs (cards, tokens, permanents)
//   - Zones (hand, deck, discard, battlefield, exile, …)
//   - Modifiers / auras with optional layered recalculation
//   - In-process synchronous event bus for trigger routing
//   - Target queries
//
// What this package does NOT do:
//   - Define what "creature" / "spell" / "damage" mean — that's
//     domain logic.
//   - Manage the cascade of triggered effects — that's
//     core.MoveContext.Queue (the action queue primitive shipped in
//     boardgame-go/core).
//   - Decide win conditions, costs, or any rule that depends on the
//     specific game.
//
// The library is optional. Importing ccg costs you nothing if your
// game isn't card-shaped; the engine never imports it. Card-shaped
// games drop a *ccg.State into their G, decorate it with their own
// fields, and call its methods from moves.
//
// Minimal sketch:
//
//	type MyG struct {
//	    *ccg.State
//	    HP map[string]int
//	}
//
//	func setupGame(_ core.Ctx, _ any) core.G {
//	    g := &MyG{State: ccg.NewState(), HP: map[string]int{"0": 30, "1": 30}}
//	    g.NewZone("hand", false)
//	    g.NewZone("deck", true)
//	    g.NewZone("battlefield", false)
//	    return g
//	}
//
//	func playCard(mc *core.MoveContext, args ...any) (core.G, error) {
//	    g := mc.G.(*MyG)
//	    id := args[0].(ccg.EntityID)
//	    g.MoveTo(id, "battlefield")
//	    g.Publish(ccg.Event{Type: "creature_summoned", Source: id})
//	    return g, nil
//	}
package ccg
