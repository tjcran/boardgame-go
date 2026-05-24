package modulebridge

import (
	"encoding/json"
	"testing"

	"github.com/tjcran/boardgame-go/modules/tabletop"
)

// TestTabletopStateWrapper_RoundTrip verifies that a fully-populated
// tabletopState (Board + Space + Terrain) survives a JSON marshal/unmarshal
// round-trip via the wrapper codec on *tabletopState.
func TestTabletopStateWrapper_RoundTrip(t *testing.T) {
	orig := newTabletopState()
	orig.Board = tabletop.NewSquareBoard(4, 4)
	orig.Space.Place(tabletop.UnitID(1), tabletop.Pos{X: 1, Y: 1})
	orig.Terrain.Tag(tabletop.Pos{X: 2, Y: 2}, "lake")

	raw, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	got := newTabletopState()
	if err := json.Unmarshal(raw, got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Board must be non-nil and have correct geometry.
	if got.Board == nil {
		t.Fatal("Board is nil after round-trip")
	}
	sq, ok := got.Board.(*tabletop.SquareBoard)
	if !ok {
		t.Fatalf("Board is %T, want *tabletop.SquareBoard", got.Board)
	}
	// Confirm geometry: chebyshev distance (0,0)→(3,3) on a 4×4 board = 3.
	if d := sq.Distance(tabletop.Pos{X: 0, Y: 0}, tabletop.Pos{X: 3, Y: 3}); d != 3 {
		t.Fatalf("Distance after round-trip = %d, want 3", d)
	}

	// Space positions must survive.
	p, ok2 := got.Space.PositionOf(tabletop.UnitID(1))
	if !ok2 || p != (tabletop.Pos{X: 1, Y: 1}) {
		t.Fatalf("Space.PositionOf(1) = %v %v, want {1,1} true", p, ok2)
	}

	// Terrain tags must survive.
	if !got.Terrain.HasTag(tabletop.Pos{X: 2, Y: 2}, "lake") {
		t.Fatal("Terrain.HasTag({2,2}, lake) = false after round-trip")
	}
}

// TestTabletopStateWrapper_NilBoardOK verifies that a nil Board does not
// cause a marshal or unmarshal error, and comes back nil.
func TestTabletopStateWrapper_NilBoardOK(t *testing.T) {
	orig := newTabletopState()
	orig.Space.Place(tabletop.UnitID(7), tabletop.Pos{X: 3, Y: 0})
	orig.Terrain.Tag(tabletop.Pos{X: 0, Y: 0}, "forest")

	raw, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal nil-board wrapper: %v", err)
	}
	got := newTabletopState()
	if err := json.Unmarshal(raw, got); err != nil {
		t.Fatalf("unmarshal nil-board wrapper: %v", err)
	}
	if got.Board != nil {
		t.Fatalf("Board = %v, want nil", got.Board)
	}
	if p, ok := got.Space.PositionOf(tabletop.UnitID(7)); !ok || p != (tabletop.Pos{X: 3, Y: 0}) {
		t.Fatalf("Space.PositionOf(7) = %v %v, want {3,0} true", p, ok)
	}
	if !got.Terrain.HasTag(tabletop.Pos{X: 0, Y: 0}, "forest") {
		t.Fatal("Terrain tag lost in nil-board round-trip")
	}
}
