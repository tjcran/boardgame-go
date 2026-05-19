package economy_test

import (
	"testing"

	"github.com/tjcran/boardgame-go/modules/ccg"
	"github.com/tjcran/boardgame-go/modules/economy"
)

// newPlayerState builds a ccg.State with one player entity and returns
// the state + entity ID for tests.
func newPlayerState() (*ccg.State, ccg.EntityID) {
	s := ccg.NewState()
	id := s.NewEntity("player", "0", nil)
	return s, id
}

func TestPoolCurrentDefaultsToZero(t *testing.T) {
	s, id := newPlayerState()
	gold := economy.Pool{Owner: id, Kind: "gold", Cap: 10}
	if got := gold.Current(s); got != 0 {
		t.Fatalf("fresh pool should have Current = 0, got %d", got)
	}
}

func TestPoolCurrentReadsCcgCounter(t *testing.T) {
	s, id := newPlayerState()
	s.AddCounter(id, "gold", 5)
	gold := economy.Pool{Owner: id, Kind: "gold", Cap: 10}
	if got := gold.Current(s); got != 5 {
		t.Fatalf("Current should reflect underlying counter, got %d (want 5)", got)
	}
}

func TestPoolGainAddsAndReportsApplied(t *testing.T) {
	s, id := newPlayerState()
	gold := economy.Pool{Owner: id, Kind: "gold", Cap: 10}
	applied := gold.Gain(s, 4)
	if applied != 4 {
		t.Errorf("Gain returned applied=%d, want 4", applied)
	}
	if gold.Current(s) != 4 {
		t.Errorf("Current = %d, want 4", gold.Current(s))
	}
}

func TestPoolGainClampsToCap(t *testing.T) {
	s, id := newPlayerState()
	gold := economy.Pool{Owner: id, Kind: "gold", Cap: 10}
	gold.Gain(s, 7)
	applied := gold.Gain(s, 5)
	// Should only gain 3 — cap is 10, current was 7.
	if applied != 3 {
		t.Errorf("Gain over cap should return applied=%d, want 3", applied)
	}
	if gold.Current(s) != 10 {
		t.Errorf("Current after capped gain = %d, want 10", gold.Current(s))
	}
}

func TestPoolGainUncappedWhenCapIsZero(t *testing.T) {
	s, id := newPlayerState()
	pile := economy.Pool{Owner: id, Kind: "shards", Cap: 0}
	pile.Gain(s, 1000)
	pile.Gain(s, 500)
	if pile.Current(s) != 1500 {
		t.Errorf("uncapped pool should accumulate, got %d", pile.Current(s))
	}
}

func TestPoolGainNegativeReturnsZero(t *testing.T) {
	s, id := newPlayerState()
	gold := economy.Pool{Owner: id, Kind: "gold", Cap: 10}
	gold.Gain(s, 5)
	applied := gold.Gain(s, -3)
	// Gain with negative n is a no-op (use Spend for reductions).
	if applied != 0 {
		t.Errorf("Gain(negative) should return 0, got %d", applied)
	}
	if gold.Current(s) != 5 {
		t.Errorf("Gain(negative) must not mutate, Current = %d (want 5)", gold.Current(s))
	}
}
