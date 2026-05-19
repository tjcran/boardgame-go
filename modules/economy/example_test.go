package economy_test

import (
	"testing"

	"github.com/tjcran/boardgame-go/modules/ccg"
	"github.com/tjcran/boardgame-go/modules/economy"
)

// TestEconomyTurnCycleWithSpendAndGain demonstrates the canonical
// deckbuilder-shape turn cycle: at turn start, Set gold to the
// per-turn income; during the turn, Spend to buy things; failed
// Spends preserve state so the move handler can return an error.
func TestEconomyTurnCycleWithSpendAndGain(t *testing.T) {
	s := ccg.NewState()
	playerID := s.NewEntity("player", "0", nil)
	gold := economy.Pool{Owner: playerID, Kind: "gold", Cap: 10}

	// Turn 1: set gold to base income (4).
	gold.Set(s, economy.Scaled(1, 4, 1, 10))
	if gold.Current(s) != 4 {
		t.Fatalf("turn 1 income = %d, want 4", gold.Current(s))
	}

	// Buy a 3-cost card: spend succeeds.
	if err := gold.Spend(s, 3); err != nil {
		t.Fatalf("buying a 3-cost card with 4 gold should succeed: %v", err)
	}
	if gold.Current(s) != 1 {
		t.Fatalf("after Spend(3), Current = %d, want 1", gold.Current(s))
	}

	// Try to buy a 5-cost card: spend fails, gold untouched.
	if err := gold.Spend(s, 5); err != economy.ErrInsufficient {
		t.Fatalf("buying a 5-cost card with 1 gold should ErrInsufficient, got %v", err)
	}
	if gold.Current(s) != 1 {
		t.Fatalf("failed Spend must not mutate, Current = %d", gold.Current(s))
	}

	// Turn 2: refresh; income grew by per.
	gold.Set(s, economy.Scaled(2, 4, 1, 10))
	if gold.Current(s) != 5 {
		t.Fatalf("turn 2 income = %d, want 5", gold.Current(s))
	}

	// Turn 99: capped at 10.
	gold.Set(s, economy.Scaled(99, 4, 1, 10))
	if gold.Current(s) != 10 {
		t.Fatalf("late-game income should cap at 10, got %d", gold.Current(s))
	}
}

// TestEconomyFiresCounterChangedEvents validates that economy
// operations route through ccg.Counters and therefore fire
// counter_changed events — so card abilities can subscribe.
func TestEconomyFiresCounterChangedEvents(t *testing.T) {
	s := ccg.NewState()
	playerID := s.NewEntity("player", "0", nil)
	gold := economy.Pool{Owner: playerID, Kind: "gold", Cap: 10}

	startEvents := len(s.Events)
	gold.Gain(s, 4)
	gold.Spend(s, 1)
	gold.Set(s, 9)
	// Each mutation publishes one counter_changed event.
	if got := len(s.Events) - startEvents; got != 3 {
		t.Errorf("expected 3 counter_changed events from Gain/Spend/Set, got %d", got)
	}
	// Last event should be the Set transition (delta = +6, total_after = 9).
	last := s.Events[len(s.Events)-1]
	if last.Type != ccg.EventCounterChanged {
		t.Errorf("last event type = %s, want %s", last.Type, ccg.EventCounterChanged)
	}
	if d, _ := last.Data["delta"].(int); d != 6 {
		t.Errorf("last event delta = %v, want 6", last.Data["delta"])
	}
}
