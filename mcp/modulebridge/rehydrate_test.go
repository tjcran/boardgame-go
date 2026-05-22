package modulebridge

import (
	"encoding/json"
	"testing"

	"github.com/tjcran/boardgame-go/modules/ccg"
	"github.com/tjcran/boardgame-go/modules/tabletop"
)

func TestRehydrate_Tabletop(t *testing.T) {
	live := newTabletopState()
	live.Board = tabletop.NewSquareBoard(4, 4)
	live.Space.Place(tabletop.UnitID(1), tabletop.Pos{X: 1, Y: 1})
	raw, _ := json.Marshal(live)
	var asMap map[string]any
	if err := json.Unmarshal(raw, &asMap); err != nil {
		t.Fatal(err)
	}
	got, err := Rehydrate("tabletop", asMap)
	if err != nil {
		t.Fatalf("rehydrate: %v", err)
	}
	ts, ok := got.(*tabletopState)
	if !ok {
		t.Fatalf("rehydrate returned %T, want *tabletopState", got)
	}
	if p, ok := ts.Space.PositionOf(tabletop.UnitID(1)); !ok || p != (tabletop.Pos{X: 1, Y: 1}) {
		t.Fatalf("position lost through rehydrate: %v %v", p, ok)
	}
	if ts.Board == nil {
		t.Fatalf("board lost through rehydrate")
	}
}

func TestRehydrate_CCG(t *testing.T) {
	live := ccg.NewState()
	live.NewZone("hand", false)
	id := live.NewEntity("card", "p1", nil)
	_ = live.MoveTo(id, "hand")

	raw, _ := json.Marshal(live)
	var asMap map[string]any
	if err := json.Unmarshal(raw, &asMap); err != nil {
		t.Fatal(err)
	}

	got, err := Rehydrate("ccg", asMap)
	if err != nil {
		t.Fatalf("rehydrate ccg: %v", err)
	}
	cs, ok := got.(*ccg.State)
	if !ok {
		t.Fatalf("rehydrate returned %T, want *ccg.State", got)
	}

	// The entity minted before rehydration must still be in the zone.
	z, ok2 := cs.Zones[ccg.ZoneName("hand")]
	if !ok2 {
		t.Fatalf("zone 'hand' lost through rehydrate")
	}
	if len(z.Members) != 1 || z.Members[0] != id {
		t.Fatalf("entity %v lost from hand after rehydrate; members=%v", id, z.Members)
	}

	// Mint a new entity on the rehydrated state; its ID must not collide
	// with the pre-rehydration entity (proves counters were restored).
	id2 := cs.NewEntity("card", "p1", nil)
	if id2 == id {
		t.Fatalf("new entity ID %v collides with pre-rehydrate ID %v", id2, id)
	}
}

func TestRehydrate_StatelessAndUnknown(t *testing.T) {
	// economy returns *emptyState (non-nil)
	gotEcon, err := Rehydrate("economy", map[string]any{})
	if err != nil {
		t.Fatalf("rehydrate economy: %v", err)
	}
	if _, ok := gotEcon.(*emptyState); !ok {
		t.Fatalf("rehydrate economy returned %T, want *emptyState", gotEcon)
	}

	// shop returns *emptyState (non-nil)
	gotShop, err := Rehydrate("shop", map[string]any{})
	if err != nil {
		t.Fatalf("rehydrate shop: %v", err)
	}
	if _, ok := gotShop.(*emptyState); !ok {
		t.Fatalf("rehydrate shop returned %T, want *emptyState", gotShop)
	}

	// unknown module returns an error
	_, err = Rehydrate("bogus", map[string]any{})
	if err == nil {
		t.Fatal("expected error for unknown module 'bogus', got nil")
	}
}
