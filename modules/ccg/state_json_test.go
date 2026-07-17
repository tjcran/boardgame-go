package ccg

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestCCGStateJSON_EmptyMapsRehydrateSafely verifies that after
// unmarshalling a state whose Entities/Zones/Modifiers were empty
// (omitempty elides them, so they decode to nil), the map-writing
// methods still work: each writer re-arms its map on the way in, so
// NewEntity/NewZone/AddModifier never panic with "assignment to entry
// in nil map".
func TestCCGStateJSON_EmptyMapsRehydrateSafely(t *testing.T) {
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

	id := got.NewEntity("card", "player0", nil)
	if id == 0 {
		t.Fatalf("expected a valid non-zero EntityID, got 0")
	}
	if _, ok := got.Entities[id]; !ok {
		t.Fatalf("entity %d missing from rehydrated Entities map", id)
	}
	if z := got.NewZone("hand", false); z == nil || got.Zones["hand"] == nil {
		t.Fatalf("NewZone on rehydrated state failed")
	}
	if mid := got.AddModifier(Modifier{Target: id, Attribute: "power", Op: OpAdd, Value: 1}); mid == 0 {
		t.Fatalf("AddModifier on rehydrated state failed")
	}
}

// TestCCGStateJSON_CountersSurvive verifies that the monotonic ID
// counters are preserved across a JSON round-trip, so that entity IDs
// minted after reload do not collide with IDs minted before.
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

// TestCCGStateJSON_EntitiesSurvive verifies that exported data
// (Entities map) is preserved intact across a JSON round-trip.
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

// TestCCGStateJSON_EmbeddedStateKeepsGameFields is the regression test
// for the embedding bug: State used to carry custom
// MarshalJSON/UnmarshalJSON methods, which Go promotes onto any game
// struct that embeds *State — hijacking the whole struct's encoding
// and silently dropping the game's own fields (the doc.go
// "type MyG struct { *ccg.State; HP … }" pattern lost HP on every
// persist). With the counters in an exported field and no custom
// codec, the default encoder sees the full outer struct.
func TestCCGStateJSON_EmbeddedStateKeepsGameFields(t *testing.T) {
	type gameG struct {
		*State
		HP map[string]int `json:"hp"`
	}
	g := &gameG{State: NewState(), HP: map[string]int{"0": 30, "1": 25}}
	id := g.NewEntity("creature", "0", nil)

	raw, err := json.Marshal(g)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"hp"`) {
		t.Fatalf("game field dropped from encoding: %s", raw)
	}

	back := &gameG{State: &State{}}
	if err := json.Unmarshal(raw, back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.HP["0"] != 30 || back.HP["1"] != 25 {
		t.Errorf("HP after round-trip = %v, want map[0:30 1:25]", back.HP)
	}
	if _, ok := back.Entities[id]; !ok {
		t.Errorf("embedded ccg state lost entity %d", id)
	}
	if back.IDs.NextEntityID != g.IDs.NextEntityID {
		t.Errorf("ID counters diverged: %+v vs %+v", back.IDs, g.IDs)
	}
}

// TestCCGStateJSON_LegacyWireShapeDecodes pins backward compatibility:
// payloads written by the replaced custom codec carried the counters
// under a nested "__counters__" key with these exact field names. The
// exported IDs field uses the same tags, so old persisted matches
// decode unchanged.
func TestCCGStateJSON_LegacyWireShapeDecodes(t *testing.T) {
	legacy := `{
		"entities": {"3": {"id": 3, "type": "creature", "owner": "0"}},
		"__counters__": {
			"nextEntityID": 3, "nextModID": 1, "nextInsertion": 2,
			"nextSubID": 0, "nextAbilityID": 4, "nextEffectID": 7
		}
	}`
	var s State
	if err := json.Unmarshal([]byte(legacy), &s); err != nil {
		t.Fatalf("unmarshal legacy payload: %v", err)
	}
	want := IDCounters{NextEntityID: 3, NextModID: 1, NextInsertion: 2, NextAbilityID: 4, NextEffectID: 7}
	if s.IDs != want {
		t.Errorf("IDs = %+v, want %+v", s.IDs, want)
	}
	if s.NewEntity("card", "0", nil) != 4 {
		t.Errorf("next minted entity ID should continue from the legacy counter")
	}
	if s.PushEffect(Effect{Kind: "x"}) != 8 {
		t.Errorf("next effect ID should continue from the legacy counter")
	}
}
