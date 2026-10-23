package ccg

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestStepSequence_BeginAdvanceToDone(t *testing.T) {
	var s StepSequence
	if err := s.Begin("round", []StepDef{{Name: "a"}, {Name: "b", Window: true}}); err != nil {
		t.Fatal(err)
	}
	if cur, ok := s.Current(); !ok || cur.Name != "a" {
		t.Fatalf("current = %+v, %v", cur, ok)
	}
	next, done, err := s.Advance()
	if err != nil || done || next.Name != "b" || !next.Window {
		t.Fatalf("advance = %+v, %v, %v", next, done, err)
	}
	_, done, err = s.Advance()
	if err != nil || !done {
		t.Fatalf("expected done, got %v, %v", done, err)
	}
	if s.Active {
		t.Fatal("still active after done")
	}
	if _, ok := s.Current(); ok {
		t.Fatal("Current should report false when inactive")
	}
}

func TestStepSequence_BeginWhileActive(t *testing.T) {
	var s StepSequence
	_ = s.Begin("x", []StepDef{{Name: "a"}})
	if err := s.Begin("y", []StepDef{{Name: "b"}}); !errors.Is(err, ErrSequenceActive) {
		t.Fatalf("err = %v", err)
	}
}

func TestStepSequence_BeginEmpty(t *testing.T) {
	var s StepSequence
	if err := s.Begin("x", nil); !errors.Is(err, ErrSequenceEmpty) {
		t.Fatalf("err = %v", err)
	}
}

func TestStepSequence_AdvanceInactive(t *testing.T) {
	var s StepSequence
	if _, _, err := s.Advance(); !errors.Is(err, ErrSequenceInactive) {
		t.Fatalf("err = %v", err)
	}
}

func TestStepSequence_Reset(t *testing.T) {
	var s StepSequence
	_ = s.Begin("x", []StepDef{{Name: "a"}})
	s.Reset()
	if s.Active {
		t.Fatal("active after reset")
	}
	if err := s.Begin("y", []StepDef{{Name: "b"}}); err != nil {
		t.Fatal(err)
	}
}

func TestStepSequence_JSONRoundTripMidSequence(t *testing.T) {
	var s StepSequence
	_ = s.Begin("x", []StepDef{{Name: "a"}, {Name: "b", Window: true}})
	_, _, _ = s.Advance()
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var back StepSequence
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if !back.Is("b") || back.Is("a") {
		t.Fatalf("after round trip: %+v", back)
	}
}

func TestStepSequence_ZeroValueOmitted(t *testing.T) {
	type holder struct {
		Seq StepSequence `json:"seq,omitzero"`
	}
	raw, err := json.Marshal(holder{})
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "{}" {
		t.Fatalf("got %s", raw)
	}
}
