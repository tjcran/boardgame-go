package economy_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/tjcran/boardgame-go/modules/ccg"
	"github.com/tjcran/boardgame-go/modules/economy"
)

// newManaState builds a state with one player entity and a basket of
// three mana pools, pre-funded per the balances map.
func newManaState(balances map[string]int) (*ccg.State, economy.Basket) {
	s := ccg.NewState()
	id := s.NewEntity("player", "0", nil)
	pools := map[string]economy.Pool{}
	for _, kind := range []string{"white", "blue", "colorless"} {
		pools[kind] = economy.Pool{Owner: id, Kind: kind}
	}
	for kind, n := range balances {
		pools[kind].Set(s, n)
	}
	return s, economy.Basket{Pools: pools}
}

// counterEvents returns the kinds of every counter_changed event in
// the state's history, in publish order.
func counterEvents(s *ccg.State) []string {
	var kinds []string
	for _, ev := range s.Events {
		if ev.Type == ccg.EventCounterChanged {
			kinds = append(kinds, ev.Data["kind"].(string))
		}
	}
	return kinds
}

func TestBasketPaySpendsAllKinds(t *testing.T) {
	s, b := newManaState(map[string]int{"white": 3, "blue": 2})
	if err := b.Pay(s, economy.Cost{"white": 2, "blue": 1}); err != nil {
		t.Fatalf("Pay within funds should not error: %v", err)
	}
	if got := b.Pools["white"].Current(s); got != 1 {
		t.Errorf("white after Pay = %d, want 1", got)
	}
	if got := b.Pools["blue"].Current(s); got != 1 {
		t.Errorf("blue after Pay = %d, want 1", got)
	}
}

func TestBasketPayShortfallLeavesStateAndEventsUntouched(t *testing.T) {
	s, b := newManaState(map[string]int{"white": 3, "blue": 0})
	eventsBefore := len(s.Events)
	err := b.Pay(s, economy.Cost{"white": 2, "blue": 1})
	if !errors.Is(err, economy.ErrInsufficient) {
		t.Fatalf("Pay over balance should wrap ErrInsufficient, got %v", err)
	}
	if !strings.Contains(err.Error(), "blue") {
		t.Errorf("error should name the offending kind, got %q", err)
	}
	if got := b.Pools["white"].Current(s); got != 3 {
		t.Errorf("failed Pay must not touch white; Current = %d (want 3)", got)
	}
	if len(s.Events) != eventsBefore {
		t.Errorf("failed Pay must publish no events; got %d new", len(s.Events)-eventsBefore)
	}
}

func TestBasketPayEventOrderIsSortedByKind(t *testing.T) {
	s, b := newManaState(map[string]int{"white": 2, "blue": 2, "colorless": 2})
	if err := b.Pay(s, economy.Cost{"white": 1, "colorless": 1, "blue": 1}); err != nil {
		t.Fatalf("Pay: %v", err)
	}
	got := counterEvents(s)
	// Setup Sets fire 3 events first; the payment appends 3 more in
	// sorted-kind order regardless of map iteration order.
	want := []string{"blue", "colorless", "white"}
	tail := got[len(got)-3:]
	for i := range want {
		if tail[i] != want[i] {
			t.Fatalf("payment event order = %v, want %v", tail, want)
		}
	}
}

func TestBasketPayZeroEntriesIgnoredAndNilCostIsFree(t *testing.T) {
	s, b := newManaState(map[string]int{"white": 1})
	if err := b.Pay(s, economy.Cost{"white": 0, "blue": 0}); err != nil {
		t.Errorf("zero-amount cost should be payable with empty pools: %v", err)
	}
	if err := b.Pay(s, nil); err != nil {
		t.Errorf("nil cost should be free: %v", err)
	}
	if got := b.Pools["white"].Current(s); got != 1 {
		t.Errorf("free costs must not mutate; white = %d (want 1)", got)
	}
}

func TestBasketPayNegativeAmountErrors(t *testing.T) {
	s, b := newManaState(map[string]int{"white": 5})
	err := b.Pay(s, economy.Cost{"white": -2})
	if !errors.Is(err, economy.ErrNegativeCost) {
		t.Fatalf("negative cost should wrap ErrNegativeCost, got %v", err)
	}
	if got := b.Pools["white"].Current(s); got != 5 {
		t.Errorf("failed Pay must not mutate; white = %d (want 5)", got)
	}
}

func TestBasketPayUnknownKindErrors(t *testing.T) {
	s, b := newManaState(map[string]int{"white": 5})
	err := b.Pay(s, economy.Cost{"green": 1})
	if !errors.Is(err, economy.ErrUnknownKind) {
		t.Fatalf("kind without pool should wrap ErrUnknownKind, got %v", err)
	}
}

func TestBasketCanPay(t *testing.T) {
	s, b := newManaState(map[string]int{"white": 2, "blue": 1})
	cases := []struct {
		name string
		cost economy.Cost
		want bool
	}{
		{"affordable", economy.Cost{"white": 2, "blue": 1}, true},
		{"short one kind", economy.Cost{"white": 2, "blue": 2}, false},
		{"unknown kind", economy.Cost{"green": 1}, false},
		{"negative amount", economy.Cost{"white": -1}, false},
		{"free", nil, true},
	}
	for _, c := range cases {
		if got := b.CanPay(s, c.cost); got != c.want {
			t.Errorf("CanPay(%s) = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestBasketCanPayDoesNotMutate(t *testing.T) {
	s, b := newManaState(map[string]int{"white": 2})
	eventsBefore := len(s.Events)
	b.CanPay(s, economy.Cost{"white": 1})
	if got := b.Pools["white"].Current(s); got != 2 {
		t.Errorf("CanPay must not mutate; white = %d (want 2)", got)
	}
	if len(s.Events) != eventsBefore {
		t.Errorf("CanPay must publish no events")
	}
}

// The {1}{W}{W} case: specific white cost must be reserved before the
// generic unit draws from the fallback, so white=2 covers exactly the
// {W}{W} and the generic falls through to colorless.
func TestBasketPayWithGenericReservesSpecificFirst(t *testing.T) {
	s, b := newManaState(map[string]int{"white": 2, "colorless": 3})
	err := b.PayWithGeneric(s, economy.Cost{"white": 2}, 1, []string{"white", "colorless"})
	if err != nil {
		t.Fatalf("PayWithGeneric: %v", err)
	}
	if got := b.Pools["white"].Current(s); got != 0 {
		t.Errorf("white after {1}{W}{W} = %d, want 0", got)
	}
	if got := b.Pools["colorless"].Current(s); got != 2 {
		t.Errorf("colorless after {1}{W}{W} = %d, want 2 (generic drawn here)", got)
	}
}

func TestBasketPayWithGenericSpansMultipleFallbackPools(t *testing.T) {
	s, b := newManaState(map[string]int{"white": 1, "blue": 1, "colorless": 1})
	err := b.PayWithGeneric(s, nil, 3, []string{"white", "blue", "colorless"})
	if err != nil {
		t.Fatalf("PayWithGeneric spanning pools: %v", err)
	}
	for _, kind := range []string{"white", "blue", "colorless"} {
		if got := b.Pools[kind].Current(s); got != 0 {
			t.Errorf("%s = %d, want 0", kind, got)
		}
	}
}

func TestBasketPayWithGenericShortfallLeavesStateUntouched(t *testing.T) {
	s, b := newManaState(map[string]int{"white": 2, "colorless": 0})
	eventsBefore := len(s.Events)
	err := b.PayWithGeneric(s, economy.Cost{"white": 2}, 1, []string{"white", "colorless"})
	if !errors.Is(err, economy.ErrInsufficient) {
		t.Fatalf("unpayable generic should wrap ErrInsufficient, got %v", err)
	}
	if got := b.Pools["white"].Current(s); got != 2 {
		t.Errorf("failed PayWithGeneric must not mutate; white = %d (want 2)", got)
	}
	if len(s.Events) != eventsBefore {
		t.Errorf("failed PayWithGeneric must publish no events")
	}
}

func TestBasketPayWithGenericDuplicateFallbackKindAddsNothing(t *testing.T) {
	s, b := newManaState(map[string]int{"white": 1, "colorless": 1})
	err := b.PayWithGeneric(s, nil, 2, []string{"white", "white", "colorless"})
	if err != nil {
		t.Fatalf("duplicate fallback kinds should be harmless: %v", err)
	}
	if got := b.Pools["white"].Current(s); got != 0 {
		t.Errorf("white = %d, want 0", got)
	}
	if got := b.Pools["colorless"].Current(s); got != 0 {
		t.Errorf("colorless = %d, want 0", got)
	}
}

func TestBasketPayWithGenericUnknownFallbackKindErrors(t *testing.T) {
	s, b := newManaState(map[string]int{"white": 5})
	err := b.PayWithGeneric(s, nil, 1, []string{"green"})
	if !errors.Is(err, economy.ErrUnknownKind) {
		t.Fatalf("unknown fallback kind should error loudly, got %v", err)
	}
	if got := b.Pools["white"].Current(s); got != 5 {
		t.Errorf("failed call must not mutate; white = %d (want 5)", got)
	}
}

func TestBasketPayWithGenericNegativeGenericErrors(t *testing.T) {
	s, b := newManaState(map[string]int{"white": 5})
	err := b.PayWithGeneric(s, nil, -1, []string{"white"})
	if !errors.Is(err, economy.ErrNegativeCost) {
		t.Fatalf("negative generic should wrap ErrNegativeCost, got %v", err)
	}
}

func TestBasketPayWithGenericZeroGenericEqualsPay(t *testing.T) {
	s, b := newManaState(map[string]int{"white": 2})
	if err := b.PayWithGeneric(s, economy.Cost{"white": 1}, 0, nil); err != nil {
		t.Fatalf("zero generic with nil fallback should behave like Pay: %v", err)
	}
	if got := b.Pools["white"].Current(s); got != 1 {
		t.Errorf("white = %d, want 1", got)
	}
}
