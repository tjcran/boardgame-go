package ccg

import (
	"encoding/json"
	"errors"
	"testing"
)

func begun(t *testing.T, s *StepSequence, name string, steps ...StepDef) {
	t.Helper()
	if err := s.Begin(name, steps); err != nil {
		t.Fatal(err)
	}
}

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
	if _, _, err := s.Advance(); !errors.Is(err, ErrSequenceInactive) {
		t.Fatalf("Advance after done: err = %v, want ErrSequenceInactive", err)
	}
}

func TestStepSequence_BeginCopiesSteps(t *testing.T) {
	var s StepSequence
	steps := []StepDef{{Name: "a"}, {Name: "b"}}
	begun(t, &s, "x", steps...)
	steps[0] = StepDef{Name: "mutated", Window: true}
	steps[1].Name = "mutated"
	if cur, ok := s.Current(); !ok || cur != (StepDef{Name: "a"}) {
		t.Fatalf("current = %+v, %v: caller mutation reached the sequence", cur, ok)
	}
	if next, _, err := s.Advance(); err != nil || next.Name != "b" {
		t.Fatalf("advance = %+v, %v", next, err)
	}
}

// TestStepSequence_AdvanceCorruptIndex: an Index outside the steps (a
// corrupt decode) is an error, never a panic, the same bound Current
// applies.
func TestStepSequence_AdvanceCorruptIndex(t *testing.T) {
	for _, idx := range []int{-1, -5, 2, 7} {
		s := StepSequence{Name: "x", Steps: []StepDef{{Name: "a"}, {Name: "b"}}, Index: idx, Active: true}
		if _, ok := s.Current(); ok {
			t.Fatalf("index %d: Current reported a step", idx)
		}
		next, done, err := s.Advance()
		if !errors.Is(err, ErrSequenceInactive) || done || next != (StepDef{}) {
			t.Fatalf("index %d: advance = %+v, %v, %v; want ErrSequenceInactive", idx, next, done, err)
		}
		if s.Index != idx || !s.Active {
			t.Fatalf("index %d: refused Advance changed the sequence: %+v", idx, s)
		}
	}
}

func TestStepSequence_BeginWhileActive(t *testing.T) {
	var s StepSequence
	begun(t, &s, "x", StepDef{Name: "a"})
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
	begun(t, &s, "x", StepDef{Name: "a"})
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
	begun(t, &s, "x", StepDef{Name: "a"}, StepDef{Name: "b", Window: true})
	if _, _, err := s.Advance(); err != nil {
		t.Fatal(err)
	}
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
