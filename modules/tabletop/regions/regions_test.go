package regions

import (
	"errors"
	"testing"

	"github.com/tjcran/boardgame-go/modules/ccg"
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

func TestInfluenceBasic(t *testing.T) {
	s := tabletop.NewState()
	s.Place(1, tabletop.Pos{0, 0}) // unit 1 in north
	s.Place(2, tabletop.Pos{1, 0}) // unit 2 in north
	s.Place(3, tabletop.Pos{0, 1}) // unit 3 in south
	owner := func(u tabletop.UnitID) string {
		switch u {
		case 1, 3:
			return "alice"
		case 2:
			return "bob"
		}
		return ""
	}
	m, _ := NewMap([]Region{
		{ID: "north", Cells: []tabletop.Pos{{0, 0}, {1, 0}}},
		{ID: "south", Cells: []tabletop.Pos{{0, 1}, {1, 1}}},
	})
	infl := m.Influence(s, owner)
	if got := infl["north"]["alice"]; got != 1 {
		t.Errorf("north/alice = %d, want 1", got)
	}
	if got := infl["north"]["bob"]; got != 1 {
		t.Errorf("north/bob = %d, want 1", got)
	}
	if got := infl["south"]["alice"]; got != 1 {
		t.Errorf("south/alice = %d, want 1", got)
	}
	if _, present := infl["south"]["bob"]; present {
		t.Errorf("south/bob present in map (value=%d), want absent", infl["south"]["bob"])
	}
}

func TestInfluenceEmptyOwnerSkipped(t *testing.T) {
	s := tabletop.NewState()
	s.Place(1, tabletop.Pos{0, 0})
	owner := func(_ tabletop.UnitID) string { return "" }
	m, _ := NewMap([]Region{{ID: "n", Cells: []tabletop.Pos{{0, 0}}}})
	infl := m.Influence(s, owner)
	if len(infl["n"]) != 0 {
		t.Errorf("expected no entries for region n, got %v", infl["n"])
	}
}

func TestInfluenceUnitOutsideAllRegions(t *testing.T) {
	s := tabletop.NewState()
	s.Place(1, tabletop.Pos{9, 9})
	owner := func(_ tabletop.UnitID) string { return "alice" }
	m, _ := NewMap([]Region{{ID: "n", Cells: []tabletop.Pos{{0, 0}}}})
	infl := m.Influence(s, owner)
	if got := infl["n"]["alice"]; got != 0 {
		t.Errorf("n/alice = %d, want 0 (unit is in no region)", got)
	}
}

func TestByCCGOwner(t *testing.T) {
	cs := ccg.NewState()
	idA := cs.NewEntity("pawn", "", map[string]any{"owner": "alice"})
	idB := cs.NewEntity("pawn", "", map[string]any{"owner": "bob"})
	idEmpty := cs.NewEntity("pawn", "", nil)

	owner := ByCCGOwner(cs)

	if got := owner(tabletop.UnitID(idA)); got != "alice" {
		t.Errorf("owner of A = %q, want alice", got)
	}
	if got := owner(tabletop.UnitID(idB)); got != "bob" {
		t.Errorf("owner of B = %q, want bob", got)
	}
	if got := owner(tabletop.UnitID(idEmpty)); got != "" {
		t.Errorf("owner of empty-attrs entity = %q, want \"\"", got)
	}
	if got := owner(tabletop.UnitID(99999)); got != "" {
		t.Errorf("owner of unknown unit = %q, want \"\"", got)
	}
}
