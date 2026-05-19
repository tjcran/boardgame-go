package shop_test

import (
	"testing"

	"github.com/tjcran/boardgame-go/core"
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

// newRng builds a *core.Random from a known seed so tests are deterministic.
func newRng(seed uint64) *core.Random {
	s := seed
	return core.NewRandomFromState(&s)
}

func TestShopFillBringsSlotsUpToSize(t *testing.T) {
	s, _, _ := newShopState(t, 0, 7) // empty market, 7 in stock
	sh := shop.Shop{Slots: "market", Stock: "stock", Size: 5}

	r := newRng(1)
	if err := sh.Fill(s, r); err != nil {
		t.Fatalf("Fill: %v", err)
	}
	if got := s.Size("market"); got != 5 {
		t.Errorf("after Fill, Slots size = %d, want 5", got)
	}
	if got := s.Size("stock"); got != 2 {
		t.Errorf("after Fill, Stock should have 7-5=2 left, got %d", got)
	}
}

func TestShopFillPartialWhenStockShortOfSize(t *testing.T) {
	s, slots, _ := newShopState(t, 1, 2) // market has 1, stock has 2, target 5
	sh := shop.Shop{Slots: "market", Stock: "stock", Size: 5}

	r := newRng(2)
	if err := sh.Fill(s, r); err != nil {
		t.Fatalf("Fill: %v", err)
	}
	// Slots had 1 + drew 2 (all of stock) = 3, short of Size=5. No error.
	if got := s.Size("market"); got != 3 {
		t.Errorf("Fill should add what stock can supply, Slots size = %d, want 3", got)
	}
	if got := s.Size("stock"); got != 0 {
		t.Errorf("Stock should be empty, got %d", got)
	}
	if !s.Contains("market", slots[0]) {
		t.Errorf("original Slot entity should still be there after Fill")
	}
}

func TestShopFillIsNoopWhenAtSize(t *testing.T) {
	s, _, _ := newShopState(t, 5, 3)
	sh := shop.Shop{Slots: "market", Stock: "stock", Size: 5}

	r := newRng(3)
	if err := sh.Fill(s, r); err != nil {
		t.Fatalf("Fill: %v", err)
	}
	if got := s.Size("stock"); got != 3 {
		t.Errorf("Fill at-Size should not touch Stock, got %d", got)
	}
}

func TestShopRollClearsThenFills(t *testing.T) {
	s, _, _ := newShopState(t, 3, 10)
	sh := shop.Shop{Slots: "market", Stock: "stock", Size: 5}

	r := newRng(4)
	if err := sh.Roll(s, r, ""); err != nil {
		t.Fatalf("Roll: %v", err)
	}
	if got := s.Size("market"); got != 5 {
		t.Errorf("after Roll, Slots should be at Size=5, got %d", got)
	}
	// 3 old (destroyed) + 5 drawn = stock down to 10-5=5.
	if got := s.Size("stock"); got != 5 {
		t.Errorf("Roll should have drawn 5 from stock, got %d remaining", got)
	}
}

func TestShopBuyMovesItemToDestination(t *testing.T) {
	s, slots, _ := newShopState(t, 3, 0)
	s.NewZone("hand", false)
	sh := shop.Shop{Slots: "market", Stock: "stock", Size: 5}

	if err := sh.Buy(s, slots[1], "hand"); err != nil {
		t.Fatalf("Buy: %v", err)
	}
	if s.Contains("market", slots[1]) {
		t.Errorf("bought item should leave Slots")
	}
	if !s.Contains("hand", slots[1]) {
		t.Errorf("bought item should land in hand")
	}
}

func TestShopBuyRejectsNonSlotEntity(t *testing.T) {
	s, _, _ := newShopState(t, 0, 0)
	s.NewZone("hand", false)
	stranger := s.NewEntity("card", "", nil)
	sh := shop.Shop{Slots: "market", Stock: "stock", Size: 5}

	if err := sh.Buy(s, stranger, "hand"); err != shop.ErrNotInSlots {
		t.Errorf("Buy on non-slot entity should return ErrNotInSlots, got %v", err)
	}
}

func TestShopBuyClearsFreezeFlag(t *testing.T) {
	s, slots, _ := newShopState(t, 2, 0)
	s.NewZone("hand", false)
	sh := shop.Shop{Slots: "market", Stock: "stock", Size: 5}
	if err := sh.Freeze(s, slots[0]); err != nil {
		t.Fatalf("Freeze: %v", err)
	}
	if err := sh.Buy(s, slots[0], "hand"); err != nil {
		t.Fatalf("Buy: %v", err)
	}
	if sh.IsFrozen(s, slots[0]) {
		t.Errorf("Buy should clear the freeze flag so the item doesn't carry shop state into the hand")
	}
}
