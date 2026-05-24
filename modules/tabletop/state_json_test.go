package tabletop

import (
	"encoding/json"
	"testing"
)

// TestTabletopState_PositionsRoundTrip confirms that default encoding/json
// round-trips State.Positions correctly and that the byCell reverse index is
// rebuilt lazily after unmarshal (so EntitiesAt works without any explicit
// index rebuild call).
func TestTabletopState_PositionsRoundTrip(t *testing.T) {
	s := NewState()
	s.Place(UnitID(1), Pos{X: 1, Y: 1})
	s.Place(UnitID(2), Pos{X: 3, Y: 0})

	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	got := NewState()
	if err := json.Unmarshal(raw, got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// PositionOf reads from Positions directly — check unit 1.
	p, ok := got.PositionOf(UnitID(1))
	if !ok || p != (Pos{X: 1, Y: 1}) {
		t.Fatalf("PositionOf(1) = %v %v, want {1,1} true", p, ok)
	}

	// EntitiesAt reads through byCell (rebuilt lazily after unmarshal).
	at := got.EntitiesAt(Pos{X: 1, Y: 1})
	if len(at) != 1 || at[0] != UnitID(1) {
		t.Fatalf("EntitiesAt({1,1}) = %v, want [1]", at)
	}

	// Second unit also survives.
	p2, ok2 := got.PositionOf(UnitID(2))
	if !ok2 || p2 != (Pos{X: 3, Y: 0}) {
		t.Fatalf("PositionOf(2) = %v %v, want {3,0} true", p2, ok2)
	}

	at2 := got.EntitiesAt(Pos{X: 3, Y: 0})
	if len(at2) != 1 || at2[0] != UnitID(2) {
		t.Fatalf("EntitiesAt({3,0}) = %v, want [2]", at2)
	}
}

// TestTabletopState_EmptyRoundTrip ensures an empty State (no units placed)
// marshals and unmarshals without error and produces a valid, usable State.
func TestTabletopState_EmptyRoundTrip(t *testing.T) {
	s := NewState()
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal empty state: %v", err)
	}
	got := NewState()
	if err := json.Unmarshal(raw, got); err != nil {
		t.Fatalf("unmarshal empty state: %v", err)
	}
	if units := got.EntitiesAt(Pos{X: 0, Y: 0}); len(units) != 0 {
		t.Fatalf("EntitiesAt on empty state = %v, want []", units)
	}
}
