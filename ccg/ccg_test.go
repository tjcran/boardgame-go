package ccg_test

import (
	"testing"

	"github.com/tjcran/boardgame-go/ccg"
)

// fixture returns a State pre-populated with two players and three
// entities — enough to exercise zones, modifiers, queries.
func fixture() *ccg.State {
	s := ccg.NewState()
	s.NewZone("hand:0", false)
	s.NewZone("hand:1", false)
	s.NewZone("battlefield", false)
	s.NewZone("deck:0", true)

	// Two creatures and a spell. Power/toughness as attrs.
	c1 := s.NewEntity("creature", "0", map[string]any{"power": 2, "toughness": 2})
	c2 := s.NewEntity("creature", "1", map[string]any{"power": 4, "toughness": 4})
	_ = s.NewEntity("spell", "0", map[string]any{"cost": 3})

	_ = s.Add("battlefield", c1)
	_ = s.Add("battlefield", c2)
	return s
}

func TestEntityCreationAssignsStableIDs(t *testing.T) {
	s := ccg.NewState()
	a := s.NewEntity("x", "0", nil)
	b := s.NewEntity("x", "0", nil)
	if a == b || a == 0 || b == 0 {
		t.Fatalf("expected distinct non-zero IDs, got %d, %d", a, b)
	}
}

func TestZoneAddRemoveContains(t *testing.T) {
	s := fixture()
	hand := ccg.ZoneName("hand:0")
	e := s.NewEntity("card", "0", nil)
	if err := s.Add(hand, e); err != nil {
		t.Fatalf("add: %v", err)
	}
	if !s.Contains(hand, e) {
		t.Fatalf("expected hand to contain %d", e)
	}
	if err := s.Remove(hand, e); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if s.Contains(hand, e) {
		t.Fatalf("expected hand to NOT contain %d after remove", e)
	}
}

func TestZoneMoveToUpdatesEntityZone(t *testing.T) {
	s := fixture()
	e := s.NewEntity("card", "0", nil)
	_ = s.Add("hand:0", e)
	if err := s.MoveTo(e, "battlefield"); err != nil {
		t.Fatalf("moveTo: %v", err)
	}
	got, _ := s.Get(e)
	if got.Zone != "battlefield" {
		t.Fatalf("expected zone updated, got %q", got.Zone)
	}
	if s.Contains("hand:0", e) {
		t.Fatalf("expected entity removed from old zone")
	}
}

func TestZoneInsertAtPositions(t *testing.T) {
	s := ccg.NewState()
	s.NewZone("deck", true)
	a := s.NewEntity("card", "0", nil)
	b := s.NewEntity("card", "0", nil)
	c := s.NewEntity("card", "0", nil)

	// Build deck [a] via Add, then insert b at the front (bottom),
	// then insert c at the end (top): expected order [b, a, c].
	if err := s.Add("deck", a); err != nil {
		t.Fatalf("add a: %v", err)
	}
	if err := s.InsertAt("deck", b, 0); err != nil {
		t.Fatalf("insert b at 0: %v", err)
	}
	if err := s.InsertAt("deck", c, 2); err != nil {
		t.Fatalf("insert c at len: %v", err)
	}
	got := s.Peek("deck", 3)
	// Peek returns top-first; top is end of Members, so the slice order
	// at Members is [b, a, c] and Peek yields [c, a, b].
	want := []ccg.EntityID{c, a, b}
	for i, id := range want {
		if got[i] != id {
			t.Fatalf("position %d: want %d, got %d (full=%v)", i, id, got[i], got)
		}
	}
}

func TestZoneInsertAtMiddle(t *testing.T) {
	s := ccg.NewState()
	s.NewZone("lane", true)
	a := s.NewEntity("card", "0", nil)
	b := s.NewEntity("card", "0", nil)
	c := s.NewEntity("card", "0", nil)
	_ = s.Add("lane", a)
	_ = s.Add("lane", b)
	// Insert c between a and b: Members becomes [a, c, b].
	if err := s.InsertAt("lane", c, 1); err != nil {
		t.Fatalf("insert at 1: %v", err)
	}
	// Peek yields top-first → [b, c, a].
	got := s.Peek("lane", 3)
	want := []ccg.EntityID{b, c, a}
	for i, id := range want {
		if got[i] != id {
			t.Fatalf("position %d: want %d, got %d (full=%v)", i, id, got[i], got)
		}
	}
}

func TestZoneInsertAtUpdatesEntityZone(t *testing.T) {
	s := ccg.NewState()
	s.NewZone("deck", true)
	id := s.NewEntity("card", "0", nil)
	if err := s.InsertAt("deck", id, 0); err != nil {
		t.Fatalf("insert: %v", err)
	}
	got, _ := s.Get(id)
	if got.Zone != "deck" {
		t.Fatalf("expected Entity.Zone updated to deck, got %q", got.Zone)
	}
}

func TestZoneInsertAtErrors(t *testing.T) {
	s := ccg.NewState()
	s.NewZone("deck", true)
	id := s.NewEntity("card", "0", nil)

	if err := s.InsertAt("missing", id, 0); err != ccg.ErrUnknownZone {
		t.Fatalf("unknown zone: want %v, got %v", ccg.ErrUnknownZone, err)
	}
	if err := s.InsertAt("deck", ccg.EntityID(9999), 0); err != ccg.ErrUnknownEntity {
		t.Fatalf("unknown entity: want %v, got %v", ccg.ErrUnknownEntity, err)
	}
	if err := s.InsertAt("deck", id, -1); err != ccg.ErrInvalidPosition {
		t.Fatalf("negative position: want %v, got %v", ccg.ErrInvalidPosition, err)
	}
	// position > len (deck is empty so any position > 0 is out of range)
	if err := s.InsertAt("deck", id, 1); err != ccg.ErrInvalidPosition {
		t.Fatalf("over-len position: want %v, got %v", ccg.ErrInvalidPosition, err)
	}
}

func TestModifierEffectiveAttrAddsAndStacks(t *testing.T) {
	s := fixture()
	creatures := ccg.Query(s).InZone("battlefield").Find()
	target := creatures[0] // first creature, base power=2

	// "+1/+1 anthem"
	_ = s.AddModifier(ccg.Modifier{
		Target: target, Attribute: "power", Op: ccg.OpAdd, Value: 1,
	})
	if got := s.EffectiveAttr(target, "power", 0); got != 3 {
		t.Fatalf("expected power=3 after +1, got %v", got)
	}
	// Second anthem (cumulative).
	_ = s.AddModifier(ccg.Modifier{
		Target: target, Attribute: "power", Op: ccg.OpAdd, Value: 2,
	})
	if got := s.EffectiveAttr(target, "power", 0); got != 5 {
		t.Fatalf("expected power=5 after two anthems, got %v", got)
	}
}

func TestModifierLayerOrderingSetBeforeAdd(t *testing.T) {
	s := fixture()
	creatures := ccg.Query(s).InZone("battlefield").Find()
	target := creatures[0] // base power=2

	// Layer 7a: "becomes a 0/2 creature" — Set replaces.
	_ = s.AddModifier(ccg.Modifier{
		Target: target, Attribute: "power", Op: ccg.OpSet, Value: 0, Layer: 7,
	})
	// Layer 8 (after Set): "+3 attack."
	_ = s.AddModifier(ccg.Modifier{
		Target: target, Attribute: "power", Op: ccg.OpAdd, Value: 3, Layer: 8,
	})
	if got := s.EffectiveAttr(target, "power", 0); got != 3 {
		t.Fatalf("expected layered apply (set 0 then +3) = 3, got %v", got)
	}
}

func TestTargetQueryFilters(t *testing.T) {
	s := fixture()
	got := ccg.Query(s).InZone("battlefield").HasType("creature").Controlled("0").Find()
	if len(got) != 1 {
		t.Fatalf("expected 1 creature controlled by 0 on battlefield, got %d", len(got))
	}
}

func TestTargetQueryAttrAtLeastUsesEffective(t *testing.T) {
	s := fixture()
	// Buff the smaller creature so it qualifies.
	creatures := ccg.Query(s).InZone("battlefield").Find()
	weak := creatures[0]
	_ = s.AddModifier(ccg.Modifier{
		Target: weak, Attribute: "power", Op: ccg.OpAdd, Value: 5,
	})
	got := ccg.Query(s).InZone("battlefield").AttrAtLeast("power", 6).Find()
	if len(got) == 0 {
		t.Fatalf("expected buffed creature to qualify, got none")
	}
}

func TestEventBusFiresMatchingSubscribers(t *testing.T) {
	s := fixture()
	var fires int
	s.Subscribe(ccg.MatchType("creature_summoned"), func(_ *ccg.State, _ ccg.Event) {
		fires++
	})
	s.Publish(ccg.Event{Type: "creature_summoned", Source: 1})
	s.Publish(ccg.Event{Type: "damage_dealt"})        // non-match
	s.Publish(ccg.Event{Type: "creature_summoned"})   // match
	if fires != 2 {
		t.Fatalf("expected 2 matching fires, got %d", fires)
	}
}

func TestEventBusDepthFirstNested(t *testing.T) {
	s := fixture()
	var order []string
	s.Subscribe(ccg.MatchType("a"), func(state *ccg.State, _ ccg.Event) {
		order = append(order, "a-handler-start")
		state.Publish(ccg.Event{Type: "b"})
		order = append(order, "a-handler-end")
	})
	s.Subscribe(ccg.MatchType("b"), func(_ *ccg.State, _ ccg.Event) {
		order = append(order, "b-handler")
	})
	s.Publish(ccg.Event{Type: "a"})

	want := []string{"a-handler-start", "b-handler", "a-handler-end"}
	if len(order) != len(want) {
		t.Fatalf("order length mismatch: got %v", order)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("step %d: got %s want %s (full=%v)", i, order[i], want[i], order)
		}
	}
}

func TestEventBufferCapped(t *testing.T) {
	s := fixture()
	for i := 0; i < ccg.MaxEventHistory*2; i++ {
		s.Publish(ccg.Event{Type: "spam"})
	}
	if len(s.Events) > ccg.MaxEventHistory {
		t.Fatalf("expected Events ≤ %d, got %d", ccg.MaxEventHistory, len(s.Events))
	}
}

func TestDestroyClearsZonesAndModifiers(t *testing.T) {
	s := fixture()
	creatures := ccg.Query(s).InZone("battlefield").Find()
	target := creatures[0]
	_ = s.AddModifier(ccg.Modifier{
		Target: target, Attribute: "power", Op: ccg.OpAdd, Value: 1,
	})
	s.Destroy(target)
	if _, ok := s.Get(target); ok {
		t.Fatalf("expected entity gone after Destroy")
	}
	if s.Contains("battlefield", target) {
		t.Fatalf("expected target removed from zone after Destroy")
	}
	for _, m := range s.Modifiers {
		if m.Target == target {
			t.Fatalf("expected modifiers targeting destroyed entity removed")
		}
	}
}
