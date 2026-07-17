package economy

import (
	"errors"
	"fmt"
	"sort"

	"github.com/tjcran/boardgame-go/modules/ccg"
)

// Cost is a multi-kind price: kind → amount. Amounts must be positive;
// zero entries are ignored, negative entries make the cost invalid.
// The zero value (nil map) is a free cost.
type Cost map[string]int

// Basket groups Pools by Kind so a multi-kind cost can be paid
// atomically. Like Pool, Basket holds no state — build one per player
// from constants (or keep it on your game config) and pass *ccg.State
// to its methods.
//
//	mana := economy.Basket{Pools: map[string]economy.Pool{
//	    "red":  {Owner: pid, Kind: "red"},
//	    "blue": {Owner: pid, Kind: "blue"},
//	}}
//	err := mana.Pay(s, economy.Cost{"red": 1, "blue": 2})
type Basket struct {
	// Pools maps a cost kind to the pool it draws from. The map key
	// is authoritative — Pool.Kind is not consulted for lookup, so a
	// basket may alias one underlying counter under several names if
	// a game needs that.
	Pools map[string]Pool
}

// ErrUnknownKind is returned when a cost names a kind the basket has
// no pool for. The state is unchanged.
var ErrUnknownKind = errors.New("economy: no pool for cost kind")

// ErrNegativeCost is returned when a cost carries a negative amount —
// that would be a silent Gain, almost always a logic bug at the call
// site. The state is unchanged.
var ErrNegativeCost = errors.New("economy: negative cost amount")

// CanPay reports whether Pay(s, cost) would succeed. False for
// invalid costs (negative amounts, kinds without pools).
func (b Basket) CanPay(s *ccg.State, cost Cost) bool {
	_, err := b.reserve(s, cost)
	return err == nil
}

// Pay spends cost all-or-nothing. The whole cost is validated against
// current balances before anything is deducted: on any failure —
// ErrNegativeCost, ErrUnknownKind, or ErrInsufficient (each wrapped
// with the offending kind) — no pool is mutated and no counter event
// fires. On success, kinds are spent in sorted-key order so the
// resulting counter_changed event sequence is deterministic.
func (b Basket) Pay(s *ccg.State, cost Cost) error {
	reserved, err := b.reserve(s, cost)
	if err != nil {
		return err
	}
	b.spendReserved(s, reserved)
	return nil
}

// PayWithGeneric spends cost plus `generic` additional units drawn
// from the basket. Specific kinds are reserved first; the generic
// remainder is then drawn greedily from fallbackOrder (a kind may
// repeat; repeats add nothing). The fallback policy — which pools
// absorb generic costs, in what order — is the caller's, typically
// from player input or a game rule; the atomicity is the library's:
// on any failure the state is untouched.
//
// generic < 0 returns ErrNegativeCost. Kinds in fallbackOrder without
// a pool return ErrUnknownKind — a silent skip would hide caller bugs.
// If the fallback pools cannot absorb the full generic amount after
// specific reservations, ErrInsufficient is returned.
func (b Basket) PayWithGeneric(s *ccg.State, cost Cost, generic int, fallbackOrder []string) error {
	if generic < 0 {
		return fmt.Errorf("%w: generic %d", ErrNegativeCost, generic)
	}
	reserved, err := b.reserve(s, cost)
	if err != nil {
		return err
	}
	need := generic
	for _, kind := range fallbackOrder {
		if need == 0 {
			break
		}
		pool, ok := b.Pools[kind]
		if !ok {
			return fmt.Errorf("%w: %q (fallback)", ErrUnknownKind, kind)
		}
		avail := pool.Current(s) - reserved[kind]
		if avail <= 0 {
			continue
		}
		take := avail
		if take > need {
			take = need
		}
		reserved[kind] += take
		need -= take
	}
	if need > 0 {
		return fmt.Errorf("%w: %d generic unpaid", ErrInsufficient, need)
	}
	b.spendReserved(s, reserved)
	return nil
}

// reserve validates cost against current balances and returns the
// per-kind amounts to spend. No mutation.
func (b Basket) reserve(s *ccg.State, cost Cost) (map[string]int, error) {
	reserved := make(map[string]int, len(cost))
	for kind, amount := range cost {
		if amount < 0 {
			return nil, fmt.Errorf("%w: %q = %d", ErrNegativeCost, kind, amount)
		}
		if amount == 0 {
			continue
		}
		pool, ok := b.Pools[kind]
		if !ok {
			return nil, fmt.Errorf("%w: %q", ErrUnknownKind, kind)
		}
		if pool.Current(s) < amount {
			return nil, fmt.Errorf("%w: %q needs %d, have %d", ErrInsufficient, kind, amount, pool.Current(s))
		}
		reserved[kind] = amount
	}
	return reserved, nil
}

// spendReserved deducts a fully validated reservation, sorted by kind
// for a deterministic event sequence. Spend cannot fail here — every
// amount was checked against Current and state is single-threaded
// within a move — so any error is a programming bug worth surfacing
// loudly rather than half-applying a payment.
func (b Basket) spendReserved(s *ccg.State, reserved map[string]int) {
	kinds := make([]string, 0, len(reserved))
	for kind := range reserved {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	for _, kind := range kinds {
		if err := b.Pools[kind].Spend(s, reserved[kind]); err != nil {
			panic(fmt.Sprintf("economy: Basket reservation invariant broken for %q: %v", kind, err))
		}
	}
}
