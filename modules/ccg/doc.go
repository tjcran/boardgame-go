// Package ccg provides bookkeeping primitives for CCG / TCG /
// deckbuilder / engine-builder games built on boardgame-go.
//
// Concerns:
//   - Entities with stable IDs (cards, tokens, permanents)
//   - Zones (hand, deck, discard, battlefield, exile, …)
//   - Modifiers / auras with optional layered recalculation
//   - In-process synchronous event bus for trigger routing
//   - Target queries
//   - Effect envelope: a declarative shape (Effect struct +
//     PendingEffects queue + optional ResolveNext dispatch) for
//     pending game actions, plus lifecycle events that subscribers
//     hook for counterspells, redirection, and replacement
//
// Effect vs core.Queue: the engine's core.MoveContext.Queue schedules
// engine moves across a cascade (FIFO, with Block / resume for
// player-input pauses). ccg.Effect represents one game-level action
// awaiting resolution inside a single move — inspectable, mutable,
// and ordered by whatever Picker the game chooses (FIFO, LIFO/stack,
// priority-window-driven). The two layer cleanly: a move pushes
// effects, drains them via ResolveNext, and the engine sees one
// atomic StateID bump.
//
// What this package does NOT do:
//   - Define what "creature" / "spell" / "damage" mean — that's
//     domain logic.
//   - Pick a resolution order for effects, or decide what
//     counterspell / redirect / replacement should mean. The library
//     ships the envelope; games compose semantics by subscribing to
//     EventEffectPushed / EventEffectResolving and calling
//     RemoveEffect, MutateEffect, or ReplaceEffect.
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
//
// Effect sketch — "deal 3 to target creature, redirectable":
//
//	// Resolver table — the game's verbs.
//	resolvers := ccg.ResolverTable{
//	    "damage": func(s *ccg.State, e ccg.Effect) error {
//	        g := s // or whatever wrapper your game uses
//	        amt, _ := e.Data["amount"].(int)
//	        g.HP[string(e.Target)] -= amt
//	        return nil
//	    },
//	}
//
//	// Static ability: redirect all damage targeting Hero to Sidekick.
//	g.Subscribe(ccg.MatchType(ccg.EventEffectResolving),
//	    func(s *ccg.State, ev ccg.Event) {
//	        id := ev.Data["effect_id"].(ccg.EffectID)
//	        s.MutateEffect(id, func(e *ccg.Effect) {
//	            if e.Kind == "damage" && e.Target == heroID {
//	                e.Target = sidekickID
//	            }
//	        })
//	    })
//
//	// Inside a move handler:
//	g.PushEffect(ccg.Effect{
//	    Kind: "damage", Source: spellID, Target: heroID,
//	    Data: map[string]any{"amount": 3},
//	})
//	for {
//	    _, ok, err := g.ResolveNext(ccg.PickFront, resolvers)
//	    if err != nil || !ok { break }
//	}
package ccg
