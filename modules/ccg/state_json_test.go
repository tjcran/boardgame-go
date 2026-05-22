package ccg

import (
	"encoding/json"
	"testing"
)

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
