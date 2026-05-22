package tabletop

import (
	"encoding/json"
	"testing"
)

func TestTabletopStateJSON_RoundTrip(t *testing.T) {
	s := NewState()
	s.Board = NewSquareBoard(4, 4)
	s.Terrain = NewTerrainMap()
	s.Place(UnitID(1), Pos{1, 1})
	s.Terrain.Tag(Pos{2, 2}, "lake")

	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := NewState()
	if err := json.Unmarshal(raw, got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if p, ok := got.PositionOf(UnitID(1)); !ok || p != (Pos{1, 1}) {
		t.Fatalf("unit position lost: %v %v", p, ok)
	}
	if got.Terrain == nil || !got.Terrain.HasTag(Pos{2, 2}, "lake") {
		t.Fatalf("terrain tag lost")
	}
	if got.Board == nil {
		t.Fatalf("board lost")
	}
}

func TestTabletopStateJSON_NilBoardOK(t *testing.T) {
	// A State with no board should still round-trip cleanly.
	s := NewState()
	s.Terrain = NewTerrainMap()
	s.Place(UnitID(5), Pos{3, 0})
	s.Terrain.Tag(Pos{0, 0}, "forest")

	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal nil-board state: %v", err)
	}
	got := NewState()
	if err := json.Unmarshal(raw, got); err != nil {
		t.Fatalf("unmarshal nil-board state: %v", err)
	}
	if got.Board != nil {
		t.Fatalf("expected nil Board after round-trip, got %v", got.Board)
	}
	if p, ok := got.PositionOf(UnitID(5)); !ok || p != (Pos{3, 0}) {
		t.Fatalf("unit position lost in nil-board round-trip: %v %v", p, ok)
	}
	if got.Terrain == nil || !got.Terrain.HasTag(Pos{0, 0}, "forest") {
		t.Fatalf("terrain tag lost in nil-board round-trip")
	}
}
