package core

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

// pairArgs spreads pairs into move args, one pair per arg, the way an
// in-process caller builds a resume MoveRequest.
func pairArgs(pairs ...[2]any) []any {
	args := make([]any, len(pairs))
	for i, p := range pairs {
		args[i] = p
	}
	return args
}

// overTheWire returns args as a transport delivers them: marshalled,
// then decoded into []any, so every pair is a []any of float64/string.
func overTheWire(t *testing.T, args []any) []any {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	var out []any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestValidateAssignment_LegalPairsAccepted(t *testing.T) {
	req := AssignmentRequest{Kind: "assign", Pairs: [][2]any{{"a", "x"}, {"b", "y"}}}
	got, err := ValidateAssignment(req, pairArgs([2]any{"b", "y"}, [2]any{"a", "x"}))
	if err != nil {
		t.Fatalf("want nil, got %v", err)
	}
	if want := [][2]any{{"b", "y"}, {"a", "x"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("pairs = %v, want %v (arg order)", got, want)
	}
}

func TestValidateAssignment_PairNotOffered(t *testing.T) {
	req := AssignmentRequest{Kind: "assign", Pairs: [][2]any{{"a", "x"}, {"b", "y"}}}
	got, err := ValidateAssignment(req, pairArgs([2]any{"a", "y"}))
	if !errors.Is(err, ErrAssignmentNotLegal) {
		t.Fatalf("want ErrAssignmentNotLegal, got %v", err)
	}
	if got != nil {
		t.Fatalf("pairs on error = %v, want nil", got)
	}
}

func TestValidateAssignment_MaxPerLeftDefaultOne(t *testing.T) {
	req := AssignmentRequest{Kind: "assign", Pairs: [][2]any{{"a", "x"}, {"a", "y"}}}
	args := pairArgs([2]any{"a", "x"}, [2]any{"a", "y"})
	if _, err := ValidateAssignment(req, args); !errors.Is(err, ErrAssignmentCardinality) {
		t.Fatalf("want ErrAssignmentCardinality, got %v", err)
	}
	req.MaxPerLeft = 2
	if _, err := ValidateAssignment(req, args); err != nil {
		t.Fatalf("MaxPerLeft 2: want nil, got %v", err)
	}
}

func TestValidateAssignment_MaxPerRight(t *testing.T) {
	req := AssignmentRequest{Kind: "assign", Pairs: [][2]any{{"a", "x"}, {"b", "x"}}}
	args := pairArgs([2]any{"a", "x"}, [2]any{"b", "x"})
	if _, err := ValidateAssignment(req, args); !errors.Is(err, ErrAssignmentCardinality) {
		t.Fatalf("want ErrAssignmentCardinality, got %v", err)
	}
	req.MaxPerRight = 2
	if _, err := ValidateAssignment(req, args); err != nil {
		t.Fatalf("MaxPerRight 2: want nil, got %v", err)
	}
}

func TestValidateAssignment_NegativeCapIsBadRequest(t *testing.T) {
	for _, req := range []AssignmentRequest{
		{Kind: "assign", Pairs: [][2]any{{"a", "x"}}, MaxPerLeft: -1},
		{Kind: "assign", Pairs: [][2]any{{"a", "x"}}, MaxPerRight: -1},
	} {
		_, err := ValidateAssignment(req, pairArgs([2]any{"a", "x"}))
		if err == nil {
			t.Fatalf("%+v: want bad-request error, got nil", req)
		}
		for _, sentinel := range []error{ErrAssignmentNotLegal, ErrAssignmentCardinality,
			ErrAssignmentEmpty, ErrAssignmentDuplicate, ErrAssignmentMalformed} {
			if errors.Is(err, sentinel) {
				t.Fatalf("%+v: bad request reported as player error %v", req, err)
			}
		}
	}
}

func TestValidateAssignment_Empty(t *testing.T) {
	req := AssignmentRequest{Kind: "assign", Pairs: [][2]any{{"a", "x"}}}
	if _, err := ValidateAssignment(req, nil); !errors.Is(err, ErrAssignmentEmpty) {
		t.Fatalf("want ErrAssignmentEmpty, got %v", err)
	}
	req.AllowEmpty = true
	got, err := ValidateAssignment(req, nil)
	if err != nil {
		t.Fatalf("AllowEmpty: want nil, got %v", err)
	}
	if got != nil {
		t.Fatalf("AllowEmpty: pairs = %v, want nil", got)
	}
}

// TestValidateAssignment_NoPairsSoftLocks pins the documented contrast
// with TargetRequest: empty Pairs is not open-ended, so without
// AllowEmpty no answer at all is accepted.
func TestValidateAssignment_NoPairsSoftLocks(t *testing.T) {
	req := AssignmentRequest{Kind: "assign"}
	if _, err := ValidateAssignment(req, nil); !errors.Is(err, ErrAssignmentEmpty) {
		t.Fatalf("empty answer: want ErrAssignmentEmpty, got %v", err)
	}
	if _, err := ValidateAssignment(req, pairArgs([2]any{"a", "x"})); !errors.Is(err, ErrAssignmentNotLegal) {
		t.Fatalf("any pair: want ErrAssignmentNotLegal, got %v", err)
	}
}

func TestValidateAssignment_Duplicate(t *testing.T) {
	req := AssignmentRequest{Kind: "assign", Pairs: [][2]any{{"a", "x"}}, MaxPerLeft: 2, MaxPerRight: 2}
	_, err := ValidateAssignment(req, pairArgs([2]any{"a", "x"}, [2]any{"a", "x"}))
	if !errors.Is(err, ErrAssignmentDuplicate) {
		t.Fatalf("want ErrAssignmentDuplicate, got %v", err)
	}
}

func TestValidateAssignment_NumericNormalization(t *testing.T) {
	req := AssignmentRequest{Kind: "assign", Pairs: [][2]any{{int(1), int(2)}}}
	if _, err := ValidateAssignment(req, pairArgs([2]any{float64(1), float64(2)})); err != nil {
		t.Fatalf("float64 vs int: want nil, got %v", err)
	}
	req = AssignmentRequest{Kind: "assign", Pairs: [][2]any{{float64(1), float64(2)}}}
	if _, err := ValidateAssignment(req, pairArgs([2]any{int64(1), int(2)})); err != nil {
		t.Fatalf("int64/int vs float64: want nil, got %v", err)
	}
}

// testEntityID stands in for a game's defined ID type.
type testEntityID uint64

// TestValidateAssignment_OverTheWire is the wire-shape regression: a
// request holding typed IDs, answered with args that went through JSON
// (pairs arrive as []any{float64, float64}), must validate and return
// the pairs, not silently read as an empty answer.
func TestValidateAssignment_OverTheWire(t *testing.T) {
	req := AssignmentRequest{Kind: "assign", Pairs: [][2]any{
		{testEntityID(3), testEntityID(1)},
		{testEntityID(4), testEntityID(2)},
	}}
	args := overTheWire(t, pairArgs([2]any{testEntityID(3), testEntityID(1)}, [2]any{testEntityID(4), testEntityID(2)}))
	if _, ok := args[0].([]any); !ok {
		t.Fatalf("decoded arg is %T, want []any", args[0])
	}
	got, err := ValidateAssignment(req, args)
	if err != nil {
		t.Fatalf("want nil, got %v", err)
	}
	if want := [][2]any{{float64(3), float64(1)}, {float64(4), float64(2)}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("pairs = %#v, want %#v", got, want)
	}

	// The same answer, still in-process, validates to the same pairs.
	native, err := ValidateAssignment(req, pairArgs([2]any{testEntityID(3), testEntityID(1)}))
	if err != nil || len(native) != 1 || native[0] != [2]any{testEntityID(3), testEntityID(1)} {
		t.Fatalf("native = %#v, %v", native, err)
	}

	// An unoffered pair is still refused after the round trip.
	bad := overTheWire(t, pairArgs([2]any{testEntityID(3), testEntityID(2)}))
	if _, err := ValidateAssignment(req, bad); !errors.Is(err, ErrAssignmentNotLegal) {
		t.Fatalf("unoffered: want ErrAssignmentNotLegal, got %v", err)
	}
}

func TestValidateAssignment_Malformed(t *testing.T) {
	req := AssignmentRequest{Kind: "assign", Pairs: [][2]any{{"a", "x"}}, AllowEmpty: true}
	for name, args := range map[string][]any{
		"whole list as one arg": {[][2]any{{"a", "x"}}},
		"one-element slice":     {[]any{"a"}},
		"three-element slice":   {[]any{"a", "x", "y"}},
		"scalar":                {"a"},
		"nil arg":               {nil},
		"good then bad":         {[2]any{"a", "x"}, "x"},
	} {
		got, err := ValidateAssignment(req, args)
		if !errors.Is(err, ErrAssignmentMalformed) {
			t.Errorf("%s: want ErrAssignmentMalformed, got %v", name, err)
		}
		if got != nil {
			t.Errorf("%s: pairs on error = %v, want nil", name, got)
		}
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
