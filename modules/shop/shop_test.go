package shop_test

import (
	"testing"

	"github.com/tjcran/boardgame-go/modules/ccg"
	"github.com/tjcran/boardgame-go/modules/shop"
)

// newShopState builds a state with the two zones a shop needs plus
// some entities placed in the Slots / Stock zones. Returns the state
// and the placed entity IDs (slot entities first, stock entities
// second) for tests to manipulate.
func newShopState(t *testing.T, slotCount, stockCount int) (*ccg.State, []ccg.EntityID, []ccg.EntityID) {
	t.Helper()
	s := ccg.NewState()
	s.NewZone("market", false)
	s.NewZone("stock", true)
	slots := make([]ccg.EntityID, slotCount)
	for i := range slots {
		slots[i] = s.NewEntity("card", "", nil)
		if err := s.Add("market", slots[i]); err != nil {
			t.Fatalf("Add(market): %v", err)
		}
	}
	stock := make([]ccg.EntityID, stockCount)
	for i := range stock {
		stock[i] = s.NewEntity("card", "", nil)
		if err := s.Add("stock", stock[i]); err != nil {
			t.Fatalf("Add(stock): %v", err)
		}
	}
	return s, slots, stock
}

func TestShopFreezeMarksEntityInSlots(t *testing.T) {
	s, slots, _ := newShopState(t, 3, 0)
	sh := shop.Shop{Slots: "market", Stock: "stock", Size: 5}

	if sh.IsFrozen(s, slots[0]) {
		t.Fatalf("fresh entity should not be frozen")
	}
	if err := sh.Freeze(s, slots[0]); err != nil {
		t.Fatalf("Freeze: %v", err)
	}
	if !sh.IsFrozen(s, slots[0]) {
		t.Fatalf("expected IsFrozen=true after Freeze")
	}
}

func TestShopFreezeRejectsNonSlotEntity(t *testing.T) {
	s, _, _ := newShopState(t, 0, 0)
	stranger := s.NewEntity("card", "", nil)
	sh := shop.Shop{Slots: "market", Stock: "stock", Size: 5}

	err := sh.Freeze(s, stranger)
	if err != shop.ErrNotInSlots {
		t.Errorf("Freeze on non-slot entity should return ErrNotInSlots, got %v", err)
	}
}

func TestShopUnfreezeClearsFlag(t *testing.T) {
	s, slots, _ := newShopState(t, 2, 0)
	sh := shop.Shop{Slots: "market", Stock: "stock", Size: 5}
	if err := sh.Freeze(s, slots[0]); err != nil {
		t.Fatalf("Freeze: %v", err)
	}
	if err := sh.Unfreeze(s, slots[0]); err != nil {
		t.Fatalf("Unfreeze: %v", err)
	}
	if sh.IsFrozen(s, slots[0]) {
		t.Fatalf("IsFrozen should be false after Unfreeze")
	}
}

func TestShopClearDestroysUnfrozenWhenNoDest(t *testing.T) {
	s, slots, _ := newShopState(t, 3, 0)
	sh := shop.Shop{Slots: "market", Stock: "stock", Size: 5}
	if err := sh.Freeze(s, slots[1]); err != nil {
		t.Fatalf("Freeze: %v", err)
	}

	if err := sh.Clear(s, ""); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	// Frozen entity stays; the other two are destroyed (gone from state).
	if !s.Contains("market", slots[1]) {
		t.Errorf("frozen entity should remain in Slots")
	}
	if _, ok := s.Get(slots[0]); ok {
		t.Errorf("un-frozen slots[0] should be destroyed")
	}
	if _, ok := s.Get(slots[2]); ok {
		t.Errorf("un-frozen slots[2] should be destroyed")
	}
}

func TestShopClearMovesUnfrozenWhenDestProvided(t *testing.T) {
	s, slots, _ := newShopState(t, 3, 0)
	s.NewZone("discard", false)
	sh := shop.Shop{Slots: "market", Stock: "stock", Size: 5}
	if err := sh.Freeze(s, slots[0]); err != nil {
		t.Fatalf("Freeze: %v", err)
	}

	if err := sh.Clear(s, "discard"); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if !s.Contains("market", slots[0]) {
		t.Errorf("frozen entity should remain in Slots")
	}
	if !s.Contains("discard", slots[1]) || !s.Contains("discard", slots[2]) {
		t.Errorf("un-frozen entities should move to discard")
	}
	// Originals are still in state (not destroyed).
	if _, ok := s.Get(slots[1]); !ok {
		t.Errorf("entity must persist when moved to dest, not destroyed")
	}
}

func TestShopClearAllFrozenIsNoOp(t *testing.T) {
	s, slots, _ := newShopState(t, 2, 0)
	sh := shop.Shop{Slots: "market", Stock: "stock", Size: 5}
	if err := sh.Freeze(s, slots[0]); err != nil {
		t.Fatalf("Freeze: %v", err)
	}
	if err := sh.Freeze(s, slots[1]); err != nil {
		t.Fatalf("Freeze: %v", err)
	}

	if err := sh.Clear(s, ""); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if s.Size("market") != 2 {
		t.Errorf("all-frozen Clear should be no-op, got Slots size %d", s.Size("market"))
	}
}
