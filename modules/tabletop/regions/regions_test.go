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
