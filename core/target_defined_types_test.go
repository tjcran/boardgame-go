package core

import "testing"

// entityIDLike mirrors the common app pattern `type EntityID uint64`.
// Candidates stored with defined numeric types must match plain
// JSON-decoded float64 selections.
type entityIDLike uint64

func TestValidateSelection_DefinedNumericCandidateTypes(t *testing.T) {
	req := TargetRequest{
		Kind:       "selectTarget",
		Candidates: []any{entityIDLike(15), entityIDLike(30)},
		Min:        1, Max: 1,
	}
	// Wire selections arrive as float64.
	if err := ValidateSelection(req, []any{float64(15)}); err != nil {
		t.Fatalf("float64 selection vs defined-type candidate: %v", err)
	}
	// Native defined-type selection (in-process callers) still matches.
	if err := ValidateSelection(req, []any{entityIDLike(30)}); err != nil {
		t.Fatalf("defined-type selection vs defined-type candidate: %v", err)
	}
	// Non-candidates still rejected.
	if err := ValidateSelection(req, []any{float64(99)}); err == nil {
		t.Fatal("expected rejection for non-candidate selection")
	}
}
