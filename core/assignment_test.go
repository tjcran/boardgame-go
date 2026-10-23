package core

import (
	"encoding/json"
	"errors"
	"strings"
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

func TestBlockSpec_AssignmentJSON(t *testing.T) {
	b := BlockSpec{Tag: "t", PlayerID: "0", Assignment: &AssignmentRequest{
		Kind: "t", Pairs: [][2]any{{"a", "x"}, {"b", "y"}},
	}}
	raw, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	var got BlockSpec
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Assignment == nil || len(got.Assignment.Pairs) != 2 || got.Assignment.Pairs[1][1] != "y" {
		t.Fatalf("round trip lost pairs: %+v", got.Assignment)
	}
	plain, _ := json.Marshal(BlockSpec{Tag: "t", PlayerID: "0"})
	if strings.Contains(string(plain), "assignment") {
		t.Fatalf("nil Assignment must be omitted: %s", plain)
	}
}
