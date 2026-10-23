package core

import (
	"errors"
	"fmt"
	"reflect"
)

// AssignmentRequest is the typed shape carried in BlockSpec.Assignment
// when a move pauses the cascade to ask a player to pair items from
// one set with items from another — "assign workers to spaces,"
// "assign defenders to attackers," "match each hero to a quest."
//
// The library owns the envelope; games own the verbs (Kind values),
// what each side of a pair means (EntityID, board position, plain
// string, …), and what the resume move does with the pairing. Pairs
// lists every legal (left, right) combination, so the game decides
// eligibility up front and the validator only has to check
// membership and counts. Pair sides are any-typed so the
// same shape covers every game-defined ID type.
//
// Pairs is a closed set. Unlike TargetRequest, whose empty Candidates
// means "open-ended, any value goes," an empty Pairs offers nothing:
// with AllowEmpty false such a request can never be answered, and the
// block soft-locks the match. Raise one with no legal pairs only when
// AllowEmpty is true.
//
// MaxPerLeft and MaxPerRight cap how many chosen pairs may share one
// left or right item; 0 means 1 (a one-to-one pairing), and a negative
// cap is a malformed request. AllowEmpty permits answering with no
// pairs at all; otherwise at least one is required.
//
// The resume move reads the request from mc.ResumingBlock.Assignment,
// validates the player's args with ValidateAssignment, and applies the
// pairs it returns.
type AssignmentRequest struct {
	Kind        string         `json:"kind"`
	Pairs       [][2]any       `json:"pairs,omitempty"`
	MaxPerLeft  int            `json:"maxPerLeft,omitempty"`
	MaxPerRight int            `json:"maxPerRight,omitempty"`
	AllowEmpty  bool           `json:"allowEmpty,omitempty"`
	Source      any            `json:"source,omitempty"`
	Data        map[string]any `json:"data,omitempty"`
}

// Sentinel errors for ValidateAssignment. Games and transports can
// surface these to clients without caring which specific reason
// triggered.
var (
	ErrAssignmentNotLegal    = errors.New("assignment pair not offered")
	ErrAssignmentCardinality = errors.New("assignment exceeds per-item limit")
	ErrAssignmentEmpty       = errors.New("assignment is empty")
	ErrAssignmentDuplicate   = errors.New("assignment repeats a pair")
	ErrAssignmentMalformed   = errors.New("assignment arg is not a pair")
)

// ValidateAssignment checks the player's move args against the request
// and returns the chosen pairs. It mirrors ValidateSelection's call
// pattern: the resume move passes its args straight through, one pair
// per arg.
//
//   - Shape: each arg is a [2]any, or a 2-element []any, which is what
//     a pair becomes after a JSON round trip (a transport decoding a
//     MoveRequest, or Replay over a stored log). Anything else is
//     ErrAssignmentMalformed.
//   - Emptiness: no args is an error unless req.AllowEmpty.
//   - Membership: every chosen pair must match an entry in req.Pairs.
//   - Uniqueness: the same pair may not be chosen twice.
//   - Cardinality: no left item appears in more than MaxPerLeft chosen
//     pairs, and no right item in more than MaxPerRight (0 means 1).
//
// A request with a negative MaxPerLeft or MaxPerRight is refused with
// a bad-request error before the args are looked at.
//
// Items compare with the same numeric normalisation as
// ValidateSelection: int, int64, float64 and defined numeric types are
// equal when they represent the same number, so a request holding
// typed IDs still matches the float64 pairs a JSON-decoded answer
// carries.
//
// The returned pairs are the player's values in arg order, decoded to
// [2]any but otherwise as received (a JSON-decoded side is still a
// float64); convert them to the game's ID type before use. nil is
// returned for a valid empty answer and on any error.
func ValidateAssignment(req AssignmentRequest, args []any) ([][2]any, error) {
	if req.MaxPerLeft < 0 || req.MaxPerRight < 0 {
		return nil, fmt.Errorf("core.ValidateAssignment: bad request MaxPerLeft=%d MaxPerRight=%d",
			req.MaxPerLeft, req.MaxPerRight)
	}
	if len(args) == 0 {
		if req.AllowEmpty {
			return nil, nil
		}
		return nil, ErrAssignmentEmpty
	}
	chosen := make([][2]any, len(args))
	for i, a := range args {
		p, ok := asPair(a)
		if !ok {
			return nil, fmt.Errorf("%w: arg %d is %T", ErrAssignmentMalformed, i, a)
		}
		chosen[i] = p
	}
	maxL, maxR := req.MaxPerLeft, req.MaxPerRight
	if maxL == 0 {
		maxL = 1
	}
	if maxR == 0 {
		maxR = 1
	}
	for i, p := range chosen {
		if !containsPair(req.Pairs, p) {
			return nil, fmt.Errorf("%w: %v -> %v", ErrAssignmentNotLegal, p[0], p[1])
		}
		for _, prev := range chosen[:i] {
			if samePair(prev, p) {
				return nil, fmt.Errorf("%w: %v -> %v", ErrAssignmentDuplicate, p[0], p[1])
			}
		}
	}
	for _, p := range chosen {
		left, right := 0, 0
		for _, o := range chosen {
			if sameItem(o[0], p[0]) {
				left++
			}
			if sameItem(o[1], p[1]) {
				right++
			}
		}
		if left > maxL {
			return nil, fmt.Errorf("%w: %v used %d times, max %d", ErrAssignmentCardinality, p[0], left, maxL)
		}
		if right > maxR {
			return nil, fmt.Errorf("%w: %v used %d times, max %d", ErrAssignmentCardinality, p[1], right, maxR)
		}
	}
	return chosen, nil
}

// asPair accepts the two shapes one pair arrives in: a Go-native [2]any
// (in-process callers) and a 2-element []any (anything decoded from
// JSON, where arrays become []any).
func asPair(v any) ([2]any, bool) {
	switch p := v.(type) {
	case [2]any:
		return p, true
	case []any:
		if len(p) == 2 {
			return [2]any{p[0], p[1]}, true
		}
	}
	return [2]any{}, false
}

func containsPair(pairs [][2]any, want [2]any) bool {
	for _, p := range pairs {
		if samePair(p, want) {
			return true
		}
	}
	return false
}

func samePair(a, b [2]any) bool {
	return sameItem(a[0], b[0]) && sameItem(a[1], b[1])
}

// sameItem compares two pair sides with numeric normalisation first
// and reflect.DeepEqual otherwise, mirroring containsCandidate.
func sameItem(a, b any) bool {
	if af, ok := asNumber(a); ok {
		bf, ok := asNumber(b)
		return ok && af == bf
	}
	return reflect.DeepEqual(a, b)
}
