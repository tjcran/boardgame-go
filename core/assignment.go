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
// left or right item; 0 means 1 (a one-to-one pairing). AllowEmpty
// permits answering with no pairs at all; otherwise at least one is
// required.
//
// The resume move reads the request from mc.ResumingBlock.Assignment,
// validates the player's pairs with ValidateAssignment, and applies
// them.
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
)

// ValidateAssignment checks the player's chosen pairs against the
// request:
//
//   - Emptiness: no pairs is an error unless req.AllowEmpty.
//   - Membership: every chosen pair must match an entry in req.Pairs.
//   - Uniqueness: the same pair may not be chosen twice.
//   - Cardinality: no left item appears in more than MaxPerLeft chosen
//     pairs, and no right item in more than MaxPerRight (0 means 1).
//
// Items compare with the same numeric normalisation as
// ValidateSelection: int, int64, and float64 are equal when they
// represent the same number, so a request validated after a JSON
// round trip still matches the player's freshly-encoded pairs.
func ValidateAssignment(req AssignmentRequest, chosen [][2]any) error {
	if len(chosen) == 0 {
		if req.AllowEmpty {
			return nil
		}
		return ErrAssignmentEmpty
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
			return fmt.Errorf("%w: %v -> %v", ErrAssignmentNotLegal, p[0], p[1])
		}
		for _, prev := range chosen[:i] {
			if samePair(prev, p) {
				return fmt.Errorf("%w: %v -> %v", ErrAssignmentDuplicate, p[0], p[1])
			}
		}
	}
	for i, p := range chosen {
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
			return fmt.Errorf("%w: %v used %d times, max %d", ErrAssignmentCardinality, chosen[i][0], left, maxL)
		}
		if right > maxR {
			return fmt.Errorf("%w: %v used %d times, max %d", ErrAssignmentCardinality, chosen[i][1], right, maxR)
		}
	}
	return nil
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
