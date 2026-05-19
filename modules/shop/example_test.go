package shop_test

import (
	"testing"

	"github.com/tjcran/boardgame-go/modules/ccg"
	"github.com/tjcran/boardgame-go/modules/economy"
	"github.com/tjcran/boardgame-go/modules/shop"
)

// TestShopAndEconomyComposeForBuy demonstrates the canonical
// shop + economy pattern: charge gold via economy, then Buy via shop.
// If the spend fails, the buy never runs; if the buy fails, the spend
// has to be refunded by the game (shop doesn't roll back).
func TestShopAndEconomyComposeForBuy(t *testing.T) {
	s := ccg.NewState()
	s.NewZone("market", false)
	s.NewZone("stock", true)
	s.NewZone("hand", false)

	// Set up a player and gold.
	playerID := s.NewEntity("player", "0", nil)
	gold := economy.Pool{Owner: playerID, Kind: "gold", Cap: 10}
	gold.Set(s, 5)

	// Put a 3-cost card in the market.
	card := s.NewEntity("card", "", map[string]any{"cost": 3})
	if err := s.Add("market", card); err != nil {
		t.Fatalf("Add: %v", err)
	}

	market := shop.Shop{Slots: "market", Stock: "stock", Size: 5}

	// Buy: check cost, spend, then move card.
	cost, _ := s.Get(card)
	if err := gold.Spend(s, cost.AttrInt("cost", 0)); err != nil {
		t.Fatalf("Spend: %v", err)
	}
	if err := market.Buy(s, card, "hand"); err != nil {
		t.Fatalf("Buy: %v", err)
	}

	if gold.Current(s) != 2 {
		t.Errorf("gold after spending 3 of 5 = %d, want 2", gold.Current(s))
	}
	if !s.Contains("hand", card) {
		t.Errorf("card should be in hand after Buy")
	}
}

// TestShopRollLifecycle demonstrates the auto-battler-shape turn:
// at turn start, Set gold to the turn's income; spend gold to reroll;
// frozen items survive the reroll; finally buy something.
func TestShopRollLifecycle(t *testing.T) {
	s := ccg.NewState()
	s.NewZone("market", false)
	s.NewZone("stock", true)
	s.NewZone("bench", false)

	// Build a stock of 12 units.
	for i := 0; i < 12; i++ {
		id := s.NewEntity("unit", "", map[string]any{"cost": 1})
		if err := s.Add("stock", id); err != nil {
			t.Fatalf("Add stock: %v", err)
		}
	}

	market := shop.Shop{Slots: "market", Stock: "stock", Size: 5}
	playerID := s.NewEntity("player", "0", nil)
	gold := economy.Pool{Owner: playerID, Kind: "gold", Cap: 10}

	r1 := newRng(1)

	// Round 1: gold = 5, fill the empty market.
	gold.Set(s, economy.Scaled(2, 4, 1, 10)) // = 5
	if err := market.Roll(s, r1, ""); err != nil {
		t.Fatalf("Roll round 1: %v", err)
	}
	if s.Size("market") != 5 {
		t.Fatalf("market should be at Size=5 after first Roll, got %d", s.Size("market"))
	}

	// Freeze the first slot; spend 1 to reroll.
	marketMembers := append([]ccg.EntityID(nil), s.Zones["market"].Members...)
	frozen := marketMembers[0]
	if err := market.Freeze(s, frozen); err != nil {
		t.Fatalf("Freeze: %v", err)
	}
	if err := gold.Spend(s, 1); err != nil {
		t.Fatalf("Spend reroll: %v", err)
	}
	if err := market.Roll(s, r1, ""); err != nil {
		t.Fatalf("Roll round 1 reroll: %v", err)
	}
	if !s.Contains("market", frozen) {
		t.Errorf("frozen unit should survive Roll, was removed")
	}
	if s.Size("market") != 5 {
		t.Errorf("Roll should leave market at Size=5, got %d", s.Size("market"))
	}

	// Buy the frozen unit — gold has 4 left (5 - 1 spent on reroll),
	// unit costs 1, so we end at 3.
	if err := gold.Spend(s, 1); err != nil {
		t.Fatalf("Spend buy: %v", err)
	}
	if err := market.Buy(s, frozen, "bench"); err != nil {
		t.Fatalf("Buy: %v", err)
	}
	if gold.Current(s) != 3 {
		t.Errorf("gold after spending 1+1 of 5 = %d, want 3", gold.Current(s))
	}
	if !s.Contains("bench", frozen) {
		t.Errorf("bought unit should be on the bench")
	}
	if market.IsFrozen(s, frozen) {
		t.Errorf("bought unit should not carry freeze flag onto bench")
	}
}
