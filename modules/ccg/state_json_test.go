package ccg

import (
	"encoding/json"
	"testing"
)

// TestCCGStateJSON_EmptyMapsRehydrateNonNil verifies that unmarshalling a
// state whose Entities/Zones/Modifiers were empty (omitempty → JSON null)
// leaves those maps non-nil, so subsequent NewEntity/zone/modifier calls do
// not panic with "assignment to entry in nil map".
func TestCCGStateJSON_EmptyMapsRehydrateNonNil(t *testing.T) {
	// A brand-new state has empty (but non-nil) maps — marshal produces
	// omitempty nulls for Entities, Zones, Modifiers.
	s := NewState()
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Unmarshal into a zero-value State (simulates what the engine does).
	var got State
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Calling NewEntity must not panic (nil-map assignment).
	var id EntityID
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("NewEntity panicked after unmarshal: %v", r)
			}
		}()
		id = got.NewEntity("card", "player0", nil)
	}()

	if id == 0 {
		t.Fatalf("expected a valid non-zero EntityID, got 0")
	}
	if _, ok := got.Entities[id]; !ok {
		t.Fatalf("entity %d missing from rehydrated Entities map", id)
	}
}

// TestCCGStateJSON_CountersSurvive verifies that unexported monotonic
// ID counters are preserved across a JSON round-trip, so that entity
// IDs minted after reload do not collide with IDs minted before.
func TestCCGStateJSON_CountersSurvive(t *testing.T) {
	s := NewState()
	id1 := s.NewEntity("card", "0", nil)
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := NewState()
	if err := json.Unmarshal(raw, got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	id2 := got.NewEntity("card", "0", nil)
	if id2 == id1 {
		t.Fatalf("entity ID collision after reload: id1=%d id2=%d (counters not persisted)", id1, id2)
	}
}

// TestCCGStateJSON_EntitiesSurvive verifies that exported data (Entities
// map) is preserved intact across a JSON round-trip after adding the
// custom codec.
func TestCCGStateJSON_EntitiesSurvive(t *testing.T) {
	s := NewState()
	id1 := s.NewEntity("creature", "alice", map[string]any{"power": 3})
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := NewState()
	if err := json.Unmarshal(raw, got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	e, ok := got.Entities[id1]
	if !ok {
		t.Fatalf("entity %d missing after round-trip; Entities=%v", id1, got.Entities)
	}
	if e.Type != "creature" {
		t.Errorf("entity type: got %q want %q", e.Type, "creature")
	}
	if e.Owner != "alice" {
		t.Errorf("entity owner: got %q want %q", e.Owner, "alice")
	}
}
