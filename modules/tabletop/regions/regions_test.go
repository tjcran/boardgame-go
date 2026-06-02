package regions

import (
	"errors"
	"testing"

	"github.com/tjcran/boardgame-go/modules/tabletop"
)

func TestNewMapValid(t *testing.T) {
	m, err := NewMap([]Region{
		{ID: "north", Cells: []tabletop.Pos{{0, 0}, {1, 0}}},
		{ID: "south", Cells: []tabletop.Pos{{0, 1}, {1, 1}}},
	})
	if err != nil {
		t.Fatalf("NewMap: %v", err)
	}
	if got := len(m.Regions); got != 2 {
		t.Fatalf("Regions len = %d, want 2", got)
	}
}

func TestNewMapEmptyID(t *testing.T) {
	_, err := NewMap([]Region{
		{ID: "", Cells: []tabletop.Pos{{0, 0}}},
	})
	if !errors.Is(err, ErrEmptyID) {
		t.Fatalf("err = %v, want ErrEmptyID", err)
	}
}

func TestNewMapDuplicateID(t *testing.T) {
	_, err := NewMap([]Region{
		{ID: "north", Cells: []tabletop.Pos{{0, 0}}},
		{ID: "north", Cells: []tabletop.Pos{{1, 0}}},
	})
	if !errors.Is(err, ErrDuplicateID) {
		t.Fatalf("err = %v, want ErrDuplicateID", err)
	}
}

func TestNewMapOverlap(t *testing.T) {
	_, err := NewMap([]Region{
		{ID: "north", Cells: []tabletop.Pos{{0, 0}, {1, 0}}},
		{ID: "south", Cells: []tabletop.Pos{{1, 0}, {2, 0}}},
	})
	if !errors.Is(err, ErrOverlap) {
		t.Fatalf("err = %v, want ErrOverlap", err)
	}
}

func TestMapOfHit(t *testing.T) {
	m, _ := NewMap([]Region{
		{ID: "north", Cells: []tabletop.Pos{{0, 0}, {1, 0}}},
		{ID: "south", Cells: []tabletop.Pos{{0, 1}, {1, 1}}},
	})
	got, ok := m.Of(tabletop.Pos{0, 0})
	if !ok || got != "north" {
		t.Fatalf("Of({0,0}) = (%q, %v), want (\"north\", true)", got, ok)
	}
}

func TestMapOfMiss(t *testing.T) {
	m, _ := NewMap([]Region{
		{ID: "north", Cells: []tabletop.Pos{{0, 0}}},
	})
	_, ok := m.Of(tabletop.Pos{9, 9})
	if ok {
		t.Fatalf("Of({9,9}) ok = true, want false (no region)")
	}
}

func TestMapCellsHit(t *testing.T) {
	m, _ := NewMap([]Region{
		{ID: "north", Cells: []tabletop.Pos{{0, 0}, {1, 0}}},
	})
	cells := m.Cells("north")
	if len(cells) != 2 {
		t.Fatalf("Cells len = %d, want 2", len(cells))
	}
	// Defensive copy: mutating the returned slice must not affect the Map.
	cells[0] = tabletop.Pos{99, 99}
	again := m.Cells("north")
	if again[0] != (tabletop.Pos{0, 0}) {
		t.Fatalf("returned slice was not defensively copied")
	}
}

func TestMapCellsMiss(t *testing.T) {
	m, _ := NewMap([]Region{{ID: "north", Cells: []tabletop.Pos{{0, 0}}}})
	if got := m.Cells("nowhere"); got != nil {
		t.Fatalf("Cells(\"nowhere\") = %v, want nil", got)
	}
}
