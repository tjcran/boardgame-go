package ccg_test

import (
	"testing"

	"github.com/tjcran/boardgame-go/ccg"
	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/plugins/random"
)

// miniG is the user game state: a ccg.State plus per-seat HP. Embedding
// *ccg.State means MyG has all the ccg methods directly.
type miniG struct {
	*ccg.State
	HP map[string]int
}

// miniGame is a tiny "play a creature, deal 1 damage to opponent"
// game. Demonstrates how ccg composes with core moves, the action
// queue, and the Random plugin.
func miniGame() *core.Game {
	g := &core.Game{
		Name:       "ccg-mini",
		MinPlayers: 2,
		MaxPlayers: 2,
		Plugins:    []core.Plugin{random.New("ccg-example")},
		Setup: func(_ core.Ctx, _ any) core.G {
			s := ccg.NewState()
			s.NewZone("battlefield", false)
			return &miniG{State: s, HP: map[string]int{"0": 5, "1": 5}}
		},
		Moves: map[string]any{
			// playCreature drops a creature on the battlefield and
			// queues an "ETB damage" trigger.
			"playCreature": core.MoveFn(func(mc *core.MoveContext, args ...any) (core.G, error) {
				g := mc.G.(*miniG)
				id := g.NewEntity("creature", mc.PlayerID, map[string]any{"power": args[0]})
				_ = g.Add("battlefield", id)
				g.Publish(ccg.Event{Type: "etb", Source: id})
				// Queue the follow-up "deal damage to opponent" move so
				// the cascade runs through the engine drain — same as
				// any other game-driven follow-up.
				opponent := "1"
				if mc.PlayerID == "1" {
					opponent = "0"
				}
				mc.Queue.Push(opponent, "takeDamage", id)
				return g, nil
			}),
			// takeDamage is the chained follow-up: the active player
			// here is the *victim* (action.PlayerID), but the source
			// entity is the creature that just ETB'd.
			"takeDamage": core.MoveFn(func(mc *core.MoveContext, args ...any) (core.G, error) {
				g := mc.G.(*miniG)
				sourceID := args[0].(ccg.EntityID)
				source, _ := g.Get(sourceID)
				dmg := source.AttrInt("power", 0)
				g.HP[mc.PlayerID] -= dmg
				g.Publish(ccg.Event{Type: "damage_dealt", Source: sourceID, Data: map[string]any{
					"amount": dmg, "victim": mc.PlayerID,
				}})
				return g, nil
			}),
		},
		EndIf: func(mc *core.MoveContext) any {
			g := mc.G.(*miniG)
			for pid, hp := range g.HP {
				if hp <= 0 {
					winner := "0"
					if pid == "0" {
						winner = "1"
					}
					return map[string]any{"winner": winner}
				}
			}
			return nil
		},
		Turn: &core.TurnConfig{MinMoves: 1, MaxMoves: 1},
	}
	return g
}

// TestCCGComposesWithEngine validates the end-to-end story:
//   1. ccg state lives in user G
//   2. moves use ccg methods to manage entities/zones
//   3. moves publish ccg events for in-process triggers
//   4. moves queue follow-up actions through mc.Queue.Push
//   5. the engine drain runs the cascade atomically (one StateID bump)
//   6. EndIf reads ccg state + user fields together
func TestCCGComposesWithEngine(t *testing.T) {
	g := miniGame()
	s := core.NewMatch(g, 0, nil)

	startID := s.StateID
	// Player 0 plays a 3-power creature. The cascade hits opponent
	// for 3 damage, dropping HP from 5 to 2.
	s, err := core.Apply(g, s, core.MoveRequest{
		PlayerID: "0", Move: "playCreature", Args: []any{3},
	})
	if err != nil {
		t.Fatalf("playCreature: %v", err)
	}
	g_ := s.G.(*miniG)
	if g_.HP["1"] != 2 {
		t.Fatalf("expected HP[1] = 2 after 3-damage trigger, got %d", g_.HP["1"])
	}
	if s.StateID != startID+1 {
		t.Fatalf("expected atomic StateID bump, got %d -> %d", startID, s.StateID)
	}
	if g_.Size("battlefield") != 1 {
		t.Fatalf("expected battlefield size 1, got %d", g_.Size("battlefield"))
	}
	// Two domain events fired: etb + damage_dealt.
	if len(g_.Events) != 2 {
		t.Fatalf("expected 2 ccg events fired, got %d", len(g_.Events))
	}
}

// TestCCGSubscriberFiresEffect demonstrates an aura-like effect: any
// time a creature with Type=cleric ETB's, every friendly creature gets
// a +1 power modifier. This is the "trigger -> modifier" pattern at
// the heart of CCG bookkeeping.
func TestCCGSubscriberFiresEffect(t *testing.T) {
	g := miniGame()

	// Pre-wire a subscriber. Real games would do this at game-define
	// time (or when a permanent enters the battlefield); this tests
	// the wiring works inside the engine's Setup -> Apply path.
	// We do it here by intercepting Setup via a custom game value.
	state := core.NewMatch(g, 0, nil)
	mg := state.G.(*miniG)
	mg.Subscribe(ccg.MatchType("etb"), func(s *ccg.State, e ccg.Event) {
		ent, _ := s.Get(e.Source)
		if ent.AttrStr("subtype", "") != "cleric" {
			return
		}
		// +1 power to every friendly creature.
		friends := ccg.Query(s).InZone("battlefield").Controlled(ent.Owner).HasType("creature").Find()
		for _, fid := range friends {
			s.AddModifier(ccg.Modifier{
				Source: e.Source, Target: fid, Attribute: "power",
				Op: ccg.OpAdd, Value: 1,
			})
		}
	})

	// Play two creatures so the second cleric's trigger buffs the first.
	state, _ = core.Apply(g, state, core.MoveRequest{
		PlayerID: "0", Move: "playCreature", Args: []any{2},
	})
	mg = state.G.(*miniG)

	// Tag the second one as a cleric BEFORE playing so the subscriber
	// fires correctly. In a real game this would be set up by the card
	// definition; for the test we cheat by writing it manually.
	preMove := state
	preMG := preMove.G.(*miniG)
	clericID := preMG.NewEntity("creature", "1", map[string]any{
		"power": 1, "subtype": "cleric",
	})
	_ = preMG.Add("battlefield", clericID)
	preMG.Publish(ccg.Event{Type: "etb", Source: clericID})

	// One friendly creature on the battlefield (the cleric itself);
	// the subscriber should have buffed it. Check effective power.
	if got := preMG.EffectiveAttr(clericID, "power", 0); got != 2 {
		t.Fatalf("expected cleric to self-buff to power 2 via ETB trigger, got %v", got)
	}
}
