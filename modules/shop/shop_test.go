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
