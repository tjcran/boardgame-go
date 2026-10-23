package ccg

import (
	"errors"
	"fmt"
	"math"
)

// Converters between typed entity pairs and the any-typed pairs that
// core.AssignmentRequest carries.
//
// A pair goes out as Go values (EntityID) but comes back from a client
// or a stored log as whatever JSON decoding produced, usually float64.
// ToAssignmentPairs builds the request side; EntityPairs turns the
// pairs core.ValidateAssignment returns back into EntityIDs, so game
// code never type-asserts decoded data.

// ErrBadPair is returned by EntityPairs when a pair side is not a value
// an EntityID can be recovered from.
var ErrBadPair = errors.New("ccg: pair side is not an entity id")

// ToAssignmentPairs converts typed (left, right) entity pairs to the
// any-typed form used by core.AssignmentRequest.Pairs, keeping order.
// nil or empty input returns nil.
func ToAssignmentPairs(pairs [][2]EntityID) [][2]any {
	if len(pairs) == 0 {
		return nil
	}
	out := make([][2]any, len(pairs))
	for i, p := range pairs {
		out[i] = [2]any{p[0], p[1]}
	}
	return out
}

// EntityPairs converts any-typed pairs, typically the result of
// core.ValidateAssignment, back to entity pairs, keeping order. Each
// side may be an EntityID, a non-negative int or int64, a uint64, or a
// non-negative integral float64 (the form a JSON round trip produces).
// Any other side, including a fractional or out-of-range float64,
// returns an error wrapping ErrBadPair and no pairs. nil or empty input
// returns nil.
func EntityPairs(pairs [][2]any) ([][2]EntityID, error) {
	if len(pairs) == 0 {
		return nil, nil
	}
	out := make([][2]EntityID, len(pairs))
	for i, p := range pairs {
		for side, v := range p {
			id, ok := toEntityID(v)
			if !ok {
				return nil, fmt.Errorf("%w: pair %d side %d is %v (%T)", ErrBadPair, i, side, v, v)
			}
			out[i][side] = id
		}
	}
	return out, nil
}

// twoTo64 is the first float64 above the uint64 range.
var twoTo64 = math.Ldexp(1, 64)

func toEntityID(v any) (EntityID, bool) {
	switch n := v.(type) {
	case EntityID:
		return n, true
	case int:
		if n >= 0 {
			return EntityID(n), true
		}
	case int64:
		if n >= 0 {
			return EntityID(n), true
		}
	case uint64:
		return EntityID(n), true
	case float64:
		if n >= 0 && n < twoTo64 && n == math.Trunc(n) {
			return EntityID(n), true
		}
	}
	return 0, false
}
