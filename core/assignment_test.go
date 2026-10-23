package core

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestValidateAssignment_LegalPairsAccepted(t *testing.T) {
	req := AssignmentRequest{Kind: "assign", Pairs: [][2]any{{"a", "x"}, {"b", "y"}}}
	if err := ValidateAssignment(req, [][2]any{{"a", "x"}, {"b", "y"}}); err != nil {
		t.Fatalf("want nil, got %v", err)
	}
}

func TestValidateAssignment_PairNotOffered(t *testing.T) {
	req := AssignmentRequest{Kind: "assign", Pairs: [][2]any{{"a", "x"}, {"b", "y"}}}
	err := ValidateAssignment(req, [][2]any{{"a", "y"}})
	if !errors.Is(err, ErrAssignmentNotLegal) {
		t.Fatalf("want ErrAssignmentNotLegal, got %v", err)
	}
}

func TestValidateAssignment_MaxPerLeftDefaultOne(t *testing.T) {
	req := AssignmentRequest{Kind: "assign", Pairs: [][2]any{{"a", "x"}, {"a", "y"}}}
	err := ValidateAssignment(req, [][2]any{{"a", "x"}, {"a", "y"}})
	if !errors.Is(err, ErrAssignmentCardinality) {
		t.Fatalf("want ErrAssignmentCardinality, got %v", err)
	}
	req.MaxPerLeft = 2
	if err := ValidateAssignment(req, [][2]any{{"a", "x"}, {"a", "y"}}); err != nil {
		t.Fatalf("MaxPerLeft 2: want nil, got %v", err)
	}
}

func TestValidateAssignment_MaxPerRight(t *testing.T) {
	req := AssignmentRequest{Kind: "assign", Pairs: [][2]any{{"a", "x"}, {"b", "x"}}}
	chosen := [][2]any{{"a", "x"}, {"b", "x"}}
	if err := ValidateAssignment(req, chosen); !errors.Is(err, ErrAssignmentCardinality) {
		t.Fatalf("want ErrAssignmentCardinality, got %v", err)
	}
	req.MaxPerRight = 2
	if err := ValidateAssignment(req, chosen); err != nil {
		t.Fatalf("MaxPerRight 2: want nil, got %v", err)
	}
}

func TestValidateAssignment_Empty(t *testing.T) {
	req := AssignmentRequest{Kind: "assign", Pairs: [][2]any{{"a", "x"}}}
	if err := ValidateAssignment(req, nil); !errors.Is(err, ErrAssignmentEmpty) {
		t.Fatalf("want ErrAssignmentEmpty, got %v", err)
	}
	req.AllowEmpty = true
	if err := ValidateAssignment(req, nil); err != nil {
		t.Fatalf("AllowEmpty: want nil, got %v", err)
	}
}

func TestValidateAssignment_Duplicate(t *testing.T) {
	req := AssignmentRequest{Kind: "assign", Pairs: [][2]any{{"a", "x"}}, MaxPerLeft: 2, MaxPerRight: 2}
	err := ValidateAssignment(req, [][2]any{{"a", "x"}, {"a", "x"}})
	if !errors.Is(err, ErrAssignmentDuplicate) {
		t.Fatalf("want ErrAssignmentDuplicate, got %v", err)
	}
}

func TestValidateAssignment_NumericNormalization(t *testing.T) {
	req := AssignmentRequest{Kind: "assign", Pairs: [][2]any{{int(1), int(2)}}}
	if err := ValidateAssignment(req, [][2]any{{float64(1), float64(2)}}); err != nil {
		t.Fatalf("want nil, got %v", err)
	}
}

func TestQueueRequestAssignment_RaisesNamedBlock(t *testing.T) {
	q := &Queue{}
	req := AssignmentRequest{Kind: "assign_defenders", Pairs: [][2]any{{"a", "x"}}}
	q.RequestAssignment("1", req, AnsweredBy("answer"))
	blocks := q.PendingBlocks()
	if len(blocks) != 1 {
		t.Fatalf("want 1 block, got %d", len(blocks))
	}
	b := blocks[0]
	if b.Tag != req.Kind || b.PlayerID != "1" || b.Move != "answer" || b.Assignment == nil {
		t.Fatalf("unexpected block: %+v", b)
	}
}

// TestBlockSpec_AssignmentJSON pins the wire bytes: core's JSON field
// names are camelCase, and every optional field drops out when unset.
func TestBlockSpec_AssignmentJSON(t *testing.T) {
	full := BlockSpec{Tag: "t", PlayerID: "0", Assignment: &AssignmentRequest{
		Kind:        "t",
		Pairs:       [][2]any{{"a", "x"}, {"b", "y"}},
		MaxPerLeft:  2,
		MaxPerRight: 3,
		AllowEmpty:  true,
		Source:      "s",
		Data:        map[string]any{"k": "v"},
	}}
	raw, err := json.Marshal(full)
	if err != nil {
		t.Fatal(err)
	}
	const wantFull = `{"tag":"t","playerID":"0","assignment":{"kind":"t","pairs":[["a","x"],["b","y"]],` +
		`"maxPerLeft":2,"maxPerRight":3,"allowEmpty":true,"source":"s","data":{"k":"v"}}}`
	if string(raw) != wantFull {
		t.Fatalf("full request bytes:\n got  %s\n want %s", raw, wantFull)
	}
	var got BlockSpec
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Assignment == nil || len(got.Assignment.Pairs) != 2 || got.Assignment.Pairs[1][1] != "y" ||
		got.Assignment.MaxPerLeft != 2 || got.Assignment.MaxPerRight != 3 || !got.Assignment.AllowEmpty {
		t.Fatalf("round trip lost fields: %+v", got.Assignment)
	}

	bare, err := json.Marshal(BlockSpec{Tag: "t", PlayerID: "0", Assignment: &AssignmentRequest{Kind: "t"}})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"tag":"t","playerID":"0","assignment":{"kind":"t"}}`; string(bare) != want {
		t.Fatalf("bare request bytes:\n got  %s\n want %s", bare, want)
	}

	plain, err := json.Marshal(BlockSpec{Tag: "t", PlayerID: "0"})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"tag":"t","playerID":"0"}`; string(plain) != want {
		t.Fatalf("nil Assignment must be omitted:\n got  %s\n want %s", plain, want)
	}
}

// TestValidateAssignment_NoPairsSoftLocks pins the documented contrast
// with TargetRequest: empty Pairs is not open-ended, so without
// AllowEmpty no answer at all is accepted.
func TestValidateAssignment_NoPairsSoftLocks(t *testing.T) {
	req := AssignmentRequest{Kind: "assign"}
	if err := ValidateAssignment(req, nil); !errors.Is(err, ErrAssignmentEmpty) {
		t.Fatalf("empty answer: want ErrAssignmentEmpty, got %v", err)
	}
	if err := ValidateAssignment(req, [][2]any{{"a", "x"}}); !errors.Is(err, ErrAssignmentNotLegal) {
		t.Fatalf("any pair: want ErrAssignmentNotLegal, got %v", err)
	}
}
