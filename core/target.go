package core

import (
	"errors"
	"fmt"
	"reflect"
)

// TargetRequest is the typed shape carried in BlockSpec.Target when a
// move pauses the cascade to ask a player to choose between known
// candidates — pawn-promotion, "pick a creature to damage," "draft
// one of these three cards," etc.
//
// The library owns the envelope; games own the verbs (Kind values),
// what the Candidates entries mean (EntityID, board position, plain
// string, …), and what the resume move does with the player's
// selection. Candidates is []any so the same shape covers every
// game-defined ID/coordinate type without requiring a type
// parameter.
//
// The resume move reads the request from mc.ResumingBlock.Target,
// validates the player's args with ValidateSelection, and applies
// the chosen targets.
type TargetRequest struct {
	Kind       string         `json:"kind"`
	Candidates []any          `json:"candidates,omitempty"`
	Min        int            `json:"min,omitempty"`
	Max        int            `json:"max,omitempty"`
	Source     any            `json:"source,omitempty"`
	Data       map[string]any `json:"data,omitempty"`
}

// Sentinel errors for ValidateSelection. Games and transports can
// surface these to clients (e.g. as 400 "bad selection") without
// caring which specific reason triggered.
var (
	ErrTargetCardinality  = errors.New("target selection count out of range")
	ErrTargetNotCandidate = errors.New("target selection not in candidate set")
)

// ValidateSelection checks the player's args against the request:
//
//   - Cardinality: len(sel) must fall in [effectiveMin, effectiveMax].
//     Min == 0 is treated as 1 (a request with no Min is implicitly
//     required); Max == 0 is treated as effectiveMin (single-pick).
//   - Membership: every selection must DeepEqual some entry in
//     req.Candidates. When Candidates is empty the request is treated
//     as open-ended (free-text coordinates, arbitrary inputs) and only
//     cardinality is checked.
//
// Numeric values are normalised before comparison: int, int64, and
// float64 all compare equal when they represent the same number.
// This matters because JSON unmarshalling turns every number into
// float64, so a TargetRequest that was serialised over the wire and
// then validated against the player's freshly-encoded selection
// would otherwise mismatch.
func ValidateSelection(req TargetRequest, sel []any) error {
	min, max := req.Min, req.Max
	if min == 0 {
		min = 1
	}
	if max == 0 {
		max = min
	}
	if max < min {
		return fmt.Errorf("core.ValidateSelection: bad request Min=%d Max=%d", req.Min, req.Max)
	}
	if len(sel) < min || len(sel) > max {
		return fmt.Errorf("%w: got %d, want %d..%d", ErrTargetCardinality, len(sel), min, max)
	}
	if len(req.Candidates) == 0 {
		return nil
	}
	for _, s := range sel {
		if !containsCandidate(req.Candidates, s) {
			return fmt.Errorf("%w: %v", ErrTargetNotCandidate, s)
		}
	}
	return nil
}

// containsCandidate reports whether want appears in cands, comparing
// via numeric normalisation first (int/int64/float64 interchangeable)
// and reflect.DeepEqual otherwise.
func containsCandidate(cands []any, want any) bool {
	if wf, ok := asNumber(want); ok {
		for _, c := range cands {
			if cf, ok := asNumber(c); ok && cf == wf {
				return true
			}
		}
		return false
	}
	for _, c := range cands {
		if reflect.DeepEqual(c, want) {
			return true
		}
	}
	return false
}

// asNumber returns v as a float64 if it's any numeric kind, or
// (0, false) otherwise. Used to bridge the JSON-decoded float64 /
// Go-native int gap when comparing candidates to selections.
//
// The type switch covers the standard kinds cheaply; the reflect
// fallback covers DEFINED numeric types (`type EntityID uint64` and
// friends) — games routinely store typed IDs in Candidates while the
// wire delivers plain float64 selections, and those must compare
// equal.
func asNumber(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int8:
		return float64(n), true
	case int16:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint8:
		return float64(n), true
	case uint16:
		return float64(n), true
	case uint32:
		return float64(n), true
	case uint64:
		return float64(n), true
	case float32:
		return float64(n), true
	case float64:
		return n, true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(rv.Int()), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return float64(rv.Uint()), true
	case reflect.Float32, reflect.Float64:
		return rv.Float(), true
	}
	return 0, false
}
