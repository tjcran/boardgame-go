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

func TestPoolSpendDeductsAndReturnsNil(t *testing.T) {
	s, id := newPlayerState()
	gold := economy.Pool{Owner: id, Kind: "gold", Cap: 10}
	gold.Gain(s, 7)
	if err := gold.Spend(s, 3); err != nil {
		t.Fatalf("Spend within funds should not error: %v", err)
	}
	if gold.Current(s) != 4 {
		t.Errorf("Current after Spend(3) = %d, want 4", gold.Current(s))
	}
}

func TestPoolSpendInsufficientErrorsAndDoesNotMutate(t *testing.T) {
	s, id := newPlayerState()
	gold := economy.Pool{Owner: id, Kind: "gold", Cap: 10}
	gold.Gain(s, 2)
	err := gold.Spend(s, 5)
	if err == nil {
		t.Fatal("Spend over balance should return error, got nil")
	}
	if err != economy.ErrInsufficient {
		t.Errorf("Spend error = %v, want ErrInsufficient", err)
	}
	if gold.Current(s) != 2 {
		t.Errorf("failed Spend must not mutate; Current = %d (want 2)", gold.Current(s))
	}
}

func TestPoolSpendZeroIsNoop(t *testing.T) {
	s, id := newPlayerState()
	gold := economy.Pool{Owner: id, Kind: "gold", Cap: 10}
	gold.Gain(s, 5)
	if err := gold.Spend(s, 0); err != nil {
		t.Errorf("Spend(0) should return nil, got %v", err)
	}
	if gold.Current(s) != 5 {
		t.Errorf("Spend(0) must not mutate, Current = %d", gold.Current(s))
	}
}

func TestPoolSpendNegativeErrors(t *testing.T) {
	s, id := newPlayerState()
	gold := economy.Pool{Owner: id, Kind: "gold", Cap: 10}
	gold.Gain(s, 5)
	if err := gold.Spend(s, -3); err == nil {
		t.Fatal("Spend(negative) should error to prevent the Spend→Gain footgun")
	}
}

func TestPoolSetOverwritesToExactValue(t *testing.T) {
	s, id := newPlayerState()
	gold := economy.Pool{Owner: id, Kind: "gold", Cap: 10}
	gold.Gain(s, 7)
	final := gold.Set(s, 4)
	if final != 4 {
		t.Errorf("Set returned %d, want 4", final)
	}
	if gold.Current(s) != 4 {
		t.Errorf("Current after Set(4) = %d, want 4", gold.Current(s))
	}
}

func TestPoolSetClampsToCap(t *testing.T) {
	s, id := newPlayerState()
	gold := economy.Pool{Owner: id, Kind: "gold", Cap: 10}
	final := gold.Set(s, 99)
	if final != 10 {
		t.Errorf("Set above cap should clamp to %d, got %d", 10, final)
	}
	if gold.Current(s) != 10 {
		t.Errorf("Current after Set(99) on cap=10 pool = %d, want 10", gold.Current(s))
	}
}

func TestPoolSetNegativeClampsToZero(t *testing.T) {
	s, id := newPlayerState()
	gold := economy.Pool{Owner: id, Kind: "gold", Cap: 10}
	gold.Gain(s, 5)
	final := gold.Set(s, -3)
	if final != 0 {
		t.Errorf("Set(negative) should clamp to 0, got %d", final)
	}
}

func TestScaledLinear(t *testing.T) {
	cases := []struct {
		turn, base, per, max, want int
	}{
		{1, 4, 1, 10, 4}, // turn 1: just base
		{2, 4, 1, 10, 5},
		{5, 4, 1, 10, 8},
		{7, 4, 1, 10, 10}, // capped
		{99, 4, 1, 10, 10},
		{1, 1, 0, 0, 1},  // no growth, no cap
		{0, 4, 1, 10, 3}, // turn 0 → base - per (edge: not all games are 1-indexed)
	}
	for _, c := range cases {
		got := economy.Scaled(c.turn, c.base, c.per, c.max)
		if got != c.want {
			t.Errorf("Scaled(turn=%d,base=%d,per=%d,max=%d) = %d, want %d",
				c.turn, c.base, c.per, c.max, got, c.want)
		}
	}
}
