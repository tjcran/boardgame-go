package ccg_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/tjcran/boardgame-go/modules/ccg"
	"github.com/tjcran/boardgame-go/core"
)

// testRandom returns a deterministic *core.Random seeded with the
// given value. Helper for tests that exercise shuffle-dependent
// behaviour and want byte-identical repeats.
func testRandom(seed uint64) *core.Random {
	return core.NewRandomFromState(&seed)
}

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

func TestZoneCapacityRejectsAddBeyondLimit(t *testing.T) {
	s := ccg.NewState()
	z := s.NewZone("lane", false)
	z.Capacity = 2
	a := s.NewEntity("creature", "0", nil)
	b := s.NewEntity("creature", "0", nil)
	c := s.NewEntity("creature", "0", nil)

	if err := s.Add("lane", a); err != nil {
		t.Fatalf("add a: %v", err)
	}
	if err := s.Add("lane", b); err != nil {
		t.Fatalf("add b: %v", err)
	}
	if err := s.Add("lane", c); err != ccg.ErrZoneFull {
		t.Fatalf("add c at cap: want ErrZoneFull, got %v", err)
	}
	if s.Size("lane") != 2 {
		t.Fatalf("zone size after rejected add: want 2, got %d", s.Size("lane"))
	}
	// Entity.Zone should NOT have been set on the rejected insert.
	got, _ := s.Get(c)
	if got.Zone != "" {
		t.Fatalf("Entity.Zone on rejected insert: want empty, got %q", got.Zone)
	}
}

func TestZoneCapacityRejectsMoveToFull(t *testing.T) {
	s := ccg.NewState()
	hand := s.NewZone("hand", false)
	hand.Capacity = 1
	s.NewZone("battlefield", false)
	a := s.NewEntity("creature", "0", nil)
	b := s.NewEntity("creature", "0", nil)
	_ = s.Add("hand", a)
	_ = s.Add("battlefield", b)

	if err := s.MoveTo(b, "hand"); err != ccg.ErrZoneFull {
		t.Fatalf("move into full hand: want ErrZoneFull, got %v", err)
	}
	// b should still be on battlefield — the move was rejected atomically.
	got, _ := s.Get(b)
	if got.Zone != "battlefield" {
		t.Fatalf("rejected move left entity in inconsistent zone: %q", got.Zone)
	}
	if !s.Contains("battlefield", b) {
		t.Fatalf("rejected move removed entity from source zone")
	}
}

func TestZoneCapacityRejectsInsertAtFull(t *testing.T) {
	s := ccg.NewState()
	z := s.NewZone("deck", true)
	z.Capacity = 2
	a := s.NewEntity("card", "0", nil)
	b := s.NewEntity("card", "0", nil)
	c := s.NewEntity("card", "0", nil)
	_ = s.Add("deck", a)
	_ = s.Add("deck", b)
	if err := s.InsertAt("deck", c, 0); err != ccg.ErrZoneFull {
		t.Fatalf("insert at cap: want ErrZoneFull, got %v", err)
	}
}

func TestZoneCapacityZeroMeansUnlimited(t *testing.T) {
	s := ccg.NewState()
	z := s.NewZone("hand", false)
	if z.Capacity != 0 {
		t.Fatalf("default Capacity: want 0, got %d", z.Capacity)
	}
	// 100 adds should all succeed when Capacity == 0 (back-compat).
	for i := 0; i < 100; i++ {
		id := s.NewEntity("card", "0", nil)
		if err := s.Add("hand", id); err != nil {
			t.Fatalf("add %d under unlimited cap: %v", i, err)
		}
	}
}

func TestZoneCapacityBoundaryAfterRemove(t *testing.T) {
	s := ccg.NewState()
	z := s.NewZone("lane", false)
	z.Capacity = 2
	a := s.NewEntity("creature", "0", nil)
	b := s.NewEntity("creature", "0", nil)
	c := s.NewEntity("creature", "0", nil)
	_ = s.Add("lane", a)
	_ = s.Add("lane", b)
	if err := s.Remove("lane", a); err != nil {
		t.Fatalf("remove: %v", err)
	}
	// One slot freed — c should fit.
	if err := s.Add("lane", c); err != nil {
		t.Fatalf("add after remove freed slot: %v", err)
	}
}

func TestZoneCapacityShrinkBelowSizeBlocksFurtherInserts(t *testing.T) {
	// Setting Capacity below current len(Members) is allowed (no
	// auto-eviction — the library never destructively edits on a config
	// write). New inserts fail until the zone shrinks below the cap.
	s := ccg.NewState()
	z := s.NewZone("lane", false)
	a := s.NewEntity("creature", "0", nil)
	b := s.NewEntity("creature", "0", nil)
	c := s.NewEntity("creature", "0", nil)
	_ = s.Add("lane", a)
	_ = s.Add("lane", b)
	_ = s.Add("lane", c)
	z.Capacity = 1 // below current len(=3); existing members stay.
	if s.Size("lane") != 3 {
		t.Fatalf("setting Capacity below size auto-evicted: now %d", s.Size("lane"))
	}
	d := s.NewEntity("creature", "0", nil)
	if err := s.Add("lane", d); err != ccg.ErrZoneFull {
		t.Fatalf("add to over-capacity zone: want ErrZoneFull, got %v", err)
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

func TestCatalogRegisterAndGet(t *testing.T) {
	c := ccg.NewCatalog()
	if c.Len() != 0 {
		t.Fatalf("empty catalog Len: want 0, got %d", c.Len())
	}
	if err := c.Register(ccg.CardDef{
		ID: "goblin", Type: "creature",
		BaseAttrs: map[string]any{"power": 2, "toughness": 1},
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	got, ok := c.Get("goblin")
	if !ok {
		t.Fatalf("Get(goblin): not found after register")
	}
	if got.Type != "creature" || got.BaseAttrs["power"] != 2 {
		t.Fatalf("Get returned wrong def: %+v", got)
	}
	if _, ok := c.Get("ghost"); ok {
		t.Fatalf("Get(ghost): unexpectedly found")
	}
	if c.Len() != 1 {
		t.Fatalf("catalog Len after 1 register: want 1, got %d", c.Len())
	}
}

func TestCatalogDuplicateRegisterIsRejected(t *testing.T) {
	c := ccg.NewCatalog()
	if err := c.Register(ccg.CardDef{ID: "x", Type: "creature"}); err != nil {
		t.Fatalf("first register: %v", err)
	}
	err := c.Register(ccg.CardDef{ID: "x", Type: "spell"})
	if err != ccg.ErrDuplicateDef {
		t.Fatalf("duplicate register: want ErrDuplicateDef, got %v", err)
	}
	// First registration must remain intact (no silent overwrite).
	got, _ := c.Get("x")
	if got.Type != "creature" {
		t.Fatalf("duplicate register clobbered original: %+v", got)
	}
}

func TestCatalogRegisterClonesBaseAttrs(t *testing.T) {
	c := ccg.NewCatalog()
	attrs := map[string]any{"power": 3}
	if err := c.Register(ccg.CardDef{ID: "x", BaseAttrs: attrs}); err != nil {
		t.Fatalf("register: %v", err)
	}
	// Mutate caller's map after registration; catalog must be unaffected.
	attrs["power"] = 99
	got, _ := c.Get("x")
	if got.BaseAttrs["power"] != 3 {
		t.Fatalf("catalog leaked caller's map mutation: %+v", got.BaseAttrs)
	}
}

func TestStateInstantiateStampsDefIDAndAttrs(t *testing.T) {
	c := ccg.NewCatalog()
	_ = c.Register(ccg.CardDef{
		ID: "goblin", Type: "creature",
		BaseAttrs: map[string]any{"power": 2, "toughness": 1},
	})
	s := ccg.NewState()
	id, err := s.Instantiate(c, "goblin", "0")
	if err != nil {
		t.Fatalf("instantiate: %v", err)
	}
	got, ok := s.Get(id)
	if !ok {
		t.Fatalf("Get after instantiate: not found")
	}
	if got.DefID != "goblin" {
		t.Fatalf("Entity.DefID: want goblin, got %q", got.DefID)
	}
	if got.Type != "creature" {
		t.Fatalf("Entity.Type: want creature, got %q", got.Type)
	}
	if got.Owner != "0" {
		t.Fatalf("Entity.Owner: want 0, got %q", got.Owner)
	}
	if got.AttrInt("power", 0) != 2 || got.AttrInt("toughness", 0) != 1 {
		t.Fatalf("Entity attrs not copied from def: %+v", got.Attrs)
	}
}

func TestStateInstantiateClonesAttrsPerInstance(t *testing.T) {
	c := ccg.NewCatalog()
	_ = c.Register(ccg.CardDef{
		ID: "goblin", Type: "creature",
		BaseAttrs: map[string]any{"power": 2},
	})
	s := ccg.NewState()
	a, _ := s.Instantiate(c, "goblin", "0")
	b, _ := s.Instantiate(c, "goblin", "1")
	// Mutate a's power; b's power must be untouched.
	s.SetAttr(a, "power", 99)
	bEnt, _ := s.Get(b)
	if bEnt.AttrInt("power", 0) != 2 {
		t.Fatalf("instances share attrs map: a mutation leaked to b (%v)", bEnt.Attrs)
	}
	// And the catalog must still report the pristine def value.
	def, _ := c.Get("goblin")
	if def.BaseAttrs["power"] != 2 {
		t.Fatalf("instance attr mutation leaked back to catalog: %+v", def.BaseAttrs)
	}
}

func TestStateInstantiateUnknownDef(t *testing.T) {
	c := ccg.NewCatalog()
	s := ccg.NewState()
	if _, err := s.Instantiate(c, "ghost", "0"); err != ccg.ErrUnknownDef {
		t.Fatalf("unknown def: want ErrUnknownDef, got %v", err)
	}
}

func TestStateInstantiateNilCatalog(t *testing.T) {
	s := ccg.NewState()
	if _, err := s.Instantiate(nil, "anything", "0"); err != ccg.ErrUnknownDef {
		t.Fatalf("nil catalog: want ErrUnknownDef, got %v", err)
	}
}

func TestEntityDefIDOmittedFromJSONWhenEmpty(t *testing.T) {
	// Entities minted via NewEntity have no DefID; omitempty should
	// keep the wire format byte-identical to pre-DefID builds.
	s := ccg.NewState()
	_ = s.NewEntity("creature", "0", nil)
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(b), "def_id") {
		t.Fatalf("def_id should be omitted when empty; got: %s", b)
	}
}

func TestEntityDefIDRoundTripsThroughJSON(t *testing.T) {
	c := ccg.NewCatalog()
	_ = c.Register(ccg.CardDef{
		ID: "goblin", Type: "creature",
		BaseAttrs: map[string]any{"power": 2},
	})
	s := ccg.NewState()
	id, _ := s.Instantiate(c, "goblin", "0")

	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(b), `"def_id":"goblin"`) {
		t.Fatalf("def_id missing from serialized state: %s", b)
	}

	var got ccg.State
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Entities[id].DefID != "goblin" {
		t.Fatalf("DefID lost in round-trip: %+v", got.Entities[id])
	}
}

func TestDeckListTotalAndCardsDeterministic(t *testing.T) {
	list := ccg.DeckList{"goblin": 3, "anthem": 2, "bolt": 4}
	if list.Total() != 9 {
		t.Fatalf("Total: want 9, got %d", list.Total())
	}
	// Run twice; result must be identical (lexicographic order).
	a := list.Cards()
	b := list.Cards()
	if len(a) != len(b) {
		t.Fatalf("Cards length not stable: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("Cards order not stable at %d: %v vs %v", i, a, b)
		}
	}
	// Lexicographic check: "anthem" before "bolt" before "goblin".
	want := []ccg.DefID{"anthem", "anthem", "bolt", "bolt", "bolt", "bolt", "goblin", "goblin", "goblin"}
	if len(a) != len(want) {
		t.Fatalf("Cards count: want %d, got %d", len(want), len(a))
	}
	for i := range want {
		if a[i] != want[i] {
			t.Fatalf("Cards[%d]: want %s, got %s (full=%v)", i, want[i], a[i], a)
		}
	}
}

func TestDeckListEmpty(t *testing.T) {
	list := ccg.DeckList{}
	if list.Total() != 0 || len(list.Cards()) != 0 {
		t.Fatalf("empty list: Total=%d Cards=%v", list.Total(), list.Cards())
	}
}

func TestDeckValidatorMinSize(t *testing.T) {
	c := ccg.NewCatalog()
	v := ccg.MinSize(5)
	if err := v.Validate(ccg.DeckList{"x": 3}, c); !errors.Is(err, ccg.ErrDeckListMinSize) {
		t.Fatalf("under-min: want ErrDeckListMinSize, got %v", err)
	}
	if err := v.Validate(ccg.DeckList{"x": 5}, c); err != nil {
		t.Fatalf("at-min: %v", err)
	}
	if err := v.Validate(ccg.DeckList{"x": 7}, c); err != nil {
		t.Fatalf("over-min: %v", err)
	}
}

func TestDeckValidatorMaxSize(t *testing.T) {
	v := ccg.MaxSize(10)
	if err := v.Validate(ccg.DeckList{"x": 11}, nil); !errors.Is(err, ccg.ErrDeckListMaxSize) {
		t.Fatalf("over-max: want ErrDeckListMaxSize, got %v", err)
	}
	if err := v.Validate(ccg.DeckList{"x": 10}, nil); err != nil {
		t.Fatalf("at-max: %v", err)
	}
}

func TestDeckValidatorMaxCopies(t *testing.T) {
	v := ccg.MaxCopies(4)
	if err := v.Validate(ccg.DeckList{"a": 4, "b": 5}, nil); !errors.Is(err, ccg.ErrDeckListMaxCopies) {
		t.Fatalf("over-copies: want ErrDeckListMaxCopies, got %v", err)
	}
	if err := v.Validate(ccg.DeckList{"a": 4, "b": 4, "c": 1}, nil); err != nil {
		t.Fatalf("at-copies: %v", err)
	}
}

func TestDeckValidatorRequireDefsExist(t *testing.T) {
	c := ccg.NewCatalog()
	_ = c.Register(ccg.CardDef{ID: "goblin", Type: "creature"})
	v := ccg.RequireDefsExist()
	if err := v.Validate(ccg.DeckList{"goblin": 4}, c); err != nil {
		t.Fatalf("known def: %v", err)
	}
	if err := v.Validate(ccg.DeckList{"goblin": 4, "ghost": 1}, c); !errors.Is(err, ccg.ErrDeckListUnknownDef) {
		t.Fatalf("unknown def: want ErrDeckListUnknownDef, got %v", err)
	}
	if err := v.Validate(ccg.DeckList{"goblin": 4}, nil); !errors.Is(err, ccg.ErrDeckListUnknownDef) {
		t.Fatalf("nil catalog: want ErrDeckListUnknownDef, got %v", err)
	}
}

func TestDeckValidatorNonNegativeCounts(t *testing.T) {
	v := ccg.NonNegativeCounts()
	if err := v.Validate(ccg.DeckList{"a": 0}, nil); !errors.Is(err, ccg.ErrDeckListNegativeCnt) {
		t.Fatalf("zero count: want ErrDeckListNegativeCnt, got %v", err)
	}
	if err := v.Validate(ccg.DeckList{"a": -1}, nil); !errors.Is(err, ccg.ErrDeckListNegativeCnt) {
		t.Fatalf("negative count: want ErrDeckListNegativeCnt, got %v", err)
	}
	if err := v.Validate(ccg.DeckList{"a": 1}, nil); err != nil {
		t.Fatalf("positive count: %v", err)
	}
}

func TestDeckValidatorCompose(t *testing.T) {
	c := ccg.NewCatalog()
	_ = c.Register(ccg.CardDef{ID: "goblin", Type: "creature"})

	v := ccg.Compose(
		ccg.RequireDefsExist(),
		ccg.MinSize(2),
		ccg.MaxSize(10),
		ccg.MaxCopies(4),
	)
	if err := v.Validate(ccg.DeckList{"goblin": 4}, c); err != nil {
		t.Fatalf("legal compose: %v", err)
	}
	// First failure wins (RequireDefsExist runs before size).
	err := v.Validate(ccg.DeckList{"ghost": 1}, c)
	if !errors.Is(err, ccg.ErrDeckListUnknownDef) {
		t.Fatalf("compose first-failure: want ErrDeckListUnknownDef, got %v", err)
	}
}

func TestLoadDeckListInsertsInDeterministicOrder(t *testing.T) {
	c := ccg.NewCatalog()
	_ = c.Register(ccg.CardDef{ID: "goblin", Type: "creature", BaseAttrs: map[string]any{"power": 2}})
	_ = c.Register(ccg.CardDef{ID: "anthem", Type: "spell"})

	list := ccg.DeckList{"goblin": 2, "anthem": 1}

	run := func() []ccg.DefID {
		s := ccg.NewState()
		s.NewZone("deck", true)
		ids, err := s.LoadDeckList(c, list, "0", "deck")
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		var defs []ccg.DefID
		for _, id := range ids {
			e, _ := s.Get(id)
			defs = append(defs, e.DefID)
		}
		return defs
	}
	a := run()
	b := run()
	if len(a) != 3 {
		t.Fatalf("expected 3 cards loaded, got %d", len(a))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("LoadDeckList not deterministic at %d: %v vs %v", i, a, b)
		}
	}
	want := []ccg.DefID{"anthem", "goblin", "goblin"}
	for i := range want {
		if a[i] != want[i] {
			t.Fatalf("position %d: want %s, got %s (full=%v)", i, want[i], a[i], a)
		}
	}
}

func TestLoadDeckListRespectsZoneCapacity(t *testing.T) {
	c := ccg.NewCatalog()
	_ = c.Register(ccg.CardDef{ID: "goblin", Type: "creature"})
	s := ccg.NewState()
	z := s.NewZone("deck", true)
	z.Capacity = 2

	_, err := s.LoadDeckList(c, ccg.DeckList{"goblin": 5}, "0", "deck")
	if !errors.Is(err, ccg.ErrZoneFull) {
		t.Fatalf("load over-capacity: want ErrZoneFull, got %v", err)
	}
	if s.Size("deck") != 2 {
		t.Fatalf("expected partial fill to 2, got %d", s.Size("deck"))
	}
}

func TestLoadDeckListUnknownDefStopsEarly(t *testing.T) {
	c := ccg.NewCatalog()
	_ = c.Register(ccg.CardDef{ID: "goblin", Type: "creature"})
	s := ccg.NewState()
	s.NewZone("deck", true)
	// "anthem" sorts before "goblin"; LoadDeckList iterates in DefID
	// order, so the unknown ref trips immediately.
	_, err := s.LoadDeckList(c, ccg.DeckList{"anthem": 1, "goblin": 1}, "0", "deck")
	if !errors.Is(err, ccg.ErrUnknownDef) {
		t.Fatalf("want ErrUnknownDef, got %v", err)
	}
}

func TestLoadDeckListSkipsZeroCounts(t *testing.T) {
	c := ccg.NewCatalog()
	_ = c.Register(ccg.CardDef{ID: "goblin", Type: "creature"})
	s := ccg.NewState()
	s.NewZone("deck", true)
	ids, err := s.LoadDeckList(c, ccg.DeckList{"goblin": 0}, "0", "deck")
	if err != nil {
		t.Fatalf("zero-count load: %v", err)
	}
	if len(ids) != 0 || s.Size("deck") != 0 {
		t.Fatalf("zero count produced %d entities", len(ids))
	}
}

func TestLoadDeckListNilCatalog(t *testing.T) {
	s := ccg.NewState()
	s.NewZone("deck", true)
	_, err := s.LoadDeckList(nil, ccg.DeckList{"x": 1}, "0", "deck")
	if !errors.Is(err, ccg.ErrUnknownDef) {
		t.Fatalf("nil catalog: want ErrUnknownDef, got %v", err)
	}
}

func TestVisibilityIsVisibleTo(t *testing.T) {
	pub := ccg.Entity{Visibility: ccg.Public, Owner: "0"}
	if !pub.IsVisibleTo("0") || !pub.IsVisibleTo("1") || !pub.IsVisibleTo("") {
		t.Fatalf("Public should be visible to everyone")
	}
	own := ccg.Entity{Visibility: ccg.OwnerOnly, Owner: "0"}
	if !own.IsVisibleTo("0") {
		t.Fatalf("OwnerOnly visible to owner: want true")
	}
	if own.IsVisibleTo("1") || own.IsVisibleTo("") {
		t.Fatalf("OwnerOnly visible to non-owner: want false")
	}
	hidden := ccg.Entity{Visibility: ccg.Hidden, Owner: "0"}
	if hidden.IsVisibleTo("0") || hidden.IsVisibleTo("1") {
		t.Fatalf("Hidden visible: want false for everyone")
	}
}

func TestSetVisibilityPersists(t *testing.T) {
	s := ccg.NewState()
	id := s.NewEntity("creature", "0", nil)
	s.SetVisibility(id, ccg.OwnerOnly)
	got, _ := s.Get(id)
	if got.Visibility != ccg.OwnerOnly {
		t.Fatalf("SetVisibility did not persist: %+v", got)
	}
	// Unknown entity is a no-op (does not panic).
	s.SetVisibility(ccg.EntityID(9999), ccg.Hidden)
}

func TestRedactForViewerCopiesPublic(t *testing.T) {
	s := ccg.NewState()
	s.NewZone("battlefield", false)
	id := s.NewEntity("creature", "0", map[string]any{"power": 3})
	_ = s.Add("battlefield", id)

	view := s.RedactForViewer("1") // not the owner
	got := view.Entities[id]
	if got.Type != "creature" || got.Owner != "0" || got.AttrInt("power", 0) != 3 {
		t.Fatalf("public entity should be copied verbatim: %+v", got)
	}
}

func TestRedactForViewerHidesOwnerOnlyForNonOwner(t *testing.T) {
	s := ccg.NewState()
	s.NewZone("hand:0", false)
	id := s.NewEntity("creature", "0", map[string]any{"power": 3})
	s.SetVisibility(id, ccg.OwnerOnly)
	_ = s.Add("hand:0", id)

	owner := s.RedactForViewer("0")
	if owner.Entities[id].Type != "creature" {
		t.Fatalf("owner should see their own card: %+v", owner.Entities[id])
	}
	if owner.Entities[id].AttrInt("power", 0) != 3 {
		t.Fatalf("owner should see their own attrs")
	}

	other := s.RedactForViewer("1")
	stub := other.Entities[id]
	if stub.Type != "" || stub.Owner != "" || len(stub.Attrs) != 0 || stub.DefID != "" {
		t.Fatalf("non-owner should see redacted stub: %+v", stub)
	}
	if stub.Zone != "hand:0" {
		t.Fatalf("zone position should leak even when entity is redacted, got %q", stub.Zone)
	}
	// Zone membership is preserved by ID.
	if !other.Contains("hand:0", id) {
		t.Fatalf("redacted entity should still appear in its zone Members")
	}
}

func TestRedactForViewerHidesHiddenFromOwnerToo(t *testing.T) {
	s := ccg.NewState()
	s.NewZone("deck:0", true)
	id := s.NewEntity("creature", "0", map[string]any{"power": 3})
	s.SetVisibility(id, ccg.Hidden)
	_ = s.Add("deck:0", id)

	// Even the owner sees a stub for Hidden.
	owner := s.RedactForViewer("0")
	stub := owner.Entities[id]
	if stub.Type != "" || stub.Owner != "" || len(stub.Attrs) != 0 {
		t.Fatalf("owner should see redacted stub for Hidden: %+v", stub)
	}
}

func TestRedactForViewerStubsModifiersOnHiddenEntities(t *testing.T) {
	s := ccg.NewState()
	s.NewZone("hand:0", false)
	id := s.NewEntity("creature", "0", map[string]any{"power": 3})
	s.SetVisibility(id, ccg.OwnerOnly)
	_ = s.Add("hand:0", id)
	modID := s.AddModifier(ccg.Modifier{
		Target: id, Attribute: "power", Op: ccg.OpAdd, Value: 99, Layer: 7, Note: "buff",
	})

	view := s.RedactForViewer("1") // non-owner
	m, ok := view.Modifiers[modID]
	if !ok {
		t.Fatalf("modifier missing from view")
	}
	if m.Attribute != "" || m.Value != nil || m.Note != "" || m.Op != "" {
		t.Fatalf("modifier should be stubbed for non-owner: %+v", m)
	}
	if m.Layer != 7 {
		t.Fatalf("modifier Layer should be preserved for ordering: got %d", m.Layer)
	}
	// Owner should still see the full modifier.
	ownerView := s.RedactForViewer("0")
	mFull := ownerView.Modifiers[modID]
	if mFull.Attribute != "power" || mFull.Value != 99 {
		t.Fatalf("owner should see full modifier: %+v", mFull)
	}
}

func TestRedactForViewerDropsEventsForRedactedEntities(t *testing.T) {
	s := ccg.NewState()
	s.NewZone("hand:0", false)
	visible := s.NewEntity("creature", "0", nil)
	hidden := s.NewEntity("creature", "0", nil)
	s.SetVisibility(hidden, ccg.OwnerOnly)

	s.Publish(ccg.Event{Type: "ping", Source: visible})
	s.Publish(ccg.Event{Type: "secret_draw", Source: hidden})
	s.Publish(ccg.Event{Type: "secret_target", Target: hidden})

	view := s.RedactForViewer("1") // non-owner
	if len(view.Events) != 1 {
		t.Fatalf("non-owner should see exactly 1 event, got %d: %+v", len(view.Events), view.Events)
	}
	if view.Events[0].Type != "ping" {
		t.Fatalf("non-owner should see only the public event, got %+v", view.Events[0])
	}

	owner := s.RedactForViewer("0")
	if len(owner.Events) != 3 {
		t.Fatalf("owner should see all 3 events, got %d", len(owner.Events))
	}
}

func TestRedactForViewerIsDeepCopy(t *testing.T) {
	s := ccg.NewState()
	s.NewZone("battlefield", false)
	id := s.NewEntity("creature", "0", map[string]any{"power": 3})
	_ = s.Add("battlefield", id)

	view := s.RedactForViewer("0")
	// Mutate the view; the original must be unaffected.
	view.Entities[id] = ccg.Entity{ID: id, Type: "tampered"}
	view.Zones["battlefield"].Members[0] = 999
	if got, _ := s.Get(id); got.Type != "creature" {
		t.Fatalf("redacted view should be a deep copy; original entity mutated: %+v", got)
	}
	if !s.Contains("battlefield", id) {
		t.Fatalf("redacted view should be a deep copy; original zone Members mutated")
	}
}

func TestEntityVisibilityOmittedFromJSONWhenDefault(t *testing.T) {
	// Public is the zero value; omitempty must keep existing entities'
	// wire format byte-identical.
	s := ccg.NewState()
	_ = s.NewEntity("creature", "0", nil)
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(b), "visibility") {
		t.Fatalf("visibility should be omitted for Public; got: %s", b)
	}
}

func TestEntityVisibilityRoundTripsThroughJSON(t *testing.T) {
	s := ccg.NewState()
	id := s.NewEntity("creature", "0", nil)
	s.SetVisibility(id, ccg.Hidden)
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(b), `"visibility":2`) {
		t.Fatalf("Hidden should serialise to numeric 2: %s", b)
	}
	var got ccg.State
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Entities[id].Visibility != ccg.Hidden {
		t.Fatalf("Visibility lost in round-trip: %+v", got.Entities[id])
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

func TestAddCounterIncrementsAndPublishes(t *testing.T) {
	s := ccg.NewState()
	id := s.NewEntity("creature", "0", nil)

	var seen []ccg.Event
	s.Subscribe(ccg.MatchType(ccg.EventCounterChanged), func(_ *ccg.State, e ccg.Event) {
		seen = append(seen, e)
	})

	s.AddCounter(id, "+1/+1", 2)
	if got := s.Counters(id, "+1/+1"); got != 2 {
		t.Fatalf("after +2: want 2, got %d", got)
	}
	s.AddCounter(id, "+1/+1", 3)
	if got := s.Counters(id, "+1/+1"); got != 5 {
		t.Fatalf("after +3 more: want 5, got %d", got)
	}
	if len(seen) != 2 {
		t.Fatalf("expected 2 events, got %d", len(seen))
	}
	last := seen[1]
	if last.Source != id {
		t.Fatalf("event Source: want %d, got %d", id, last.Source)
	}
	if last.Data["kind"] != "+1/+1" {
		t.Fatalf("event kind: %v", last.Data["kind"])
	}
	if last.Data["delta"] != 3 {
		t.Fatalf("event delta: want 3, got %v", last.Data["delta"])
	}
	if last.Data["total_after"] != 5 {
		t.Fatalf("event total_after: want 5, got %v", last.Data["total_after"])
	}
}

func TestAddCounterClampsAtZero(t *testing.T) {
	s := ccg.NewState()
	id := s.NewEntity("creature", "0", nil)
	s.AddCounter(id, "x", 3)

	var events []ccg.Event
	s.Subscribe(ccg.MatchType(ccg.EventCounterChanged), func(_ *ccg.State, e ccg.Event) {
		events = append(events, e)
	})

	// Try to remove 10 from a total of 3 — delta should report as -3.
	s.AddCounter(id, "x", -10)
	if got := s.Counters(id, "x"); got != 0 {
		t.Fatalf("clamp at 0: got %d", got)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Data["delta"] != -3 {
		t.Fatalf("applied delta: want -3, got %v", events[0].Data["delta"])
	}
	if events[0].Data["total_after"] != 0 {
		t.Fatalf("total_after: want 0, got %v", events[0].Data["total_after"])
	}
}

func TestAddCounterNoOpDoesNotPublish(t *testing.T) {
	s := ccg.NewState()
	id := s.NewEntity("creature", "0", nil)

	var fires int
	s.Subscribe(ccg.MatchType(ccg.EventCounterChanged), func(_ *ccg.State, _ ccg.Event) {
		fires++
	})

	s.AddCounter(id, "x", 0)
	// Removing from zero clamps to zero — applied delta is 0, no event.
	s.AddCounter(id, "x", -5)

	if fires != 0 {
		t.Fatalf("expected 0 events on no-op, got %d", fires)
	}
}

func TestAddCounterUnknownEntityIsNoOp(t *testing.T) {
	s := ccg.NewState()
	var fires int
	s.Subscribe(ccg.MatchType(ccg.EventCounterChanged), func(_ *ccg.State, _ ccg.Event) {
		fires++
	})

	s.AddCounter(ccg.EntityID(9999), "x", 5)
	if fires != 0 {
		t.Fatalf("unknown entity: expected 0 events, got %d", fires)
	}
}

func TestRemoveCounterSugar(t *testing.T) {
	s := ccg.NewState()
	id := s.NewEntity("creature", "0", nil)
	s.AddCounter(id, "loyalty", 4)
	s.RemoveCounter(id, "loyalty", 1)
	if got := s.Counters(id, "loyalty"); got != 3 {
		t.Fatalf("after -1: want 3, got %d", got)
	}
	// Negative-n argument: no-op (does not silently add).
	s.RemoveCounter(id, "loyalty", -5)
	if got := s.Counters(id, "loyalty"); got != 3 {
		t.Fatalf("negative arg should be no-op, got %d", got)
	}
}

func TestCountersZeroForUnknown(t *testing.T) {
	s := ccg.NewState()
	if got := s.Counters(ccg.EntityID(9999), "x"); got != 0 {
		t.Fatalf("unknown entity: want 0, got %d", got)
	}
	id := s.NewEntity("creature", "0", nil)
	if got := s.Counters(id, "unset"); got != 0 {
		t.Fatalf("unset counter: want 0, got %d", got)
	}
}

func TestAllCountersReturnsCopy(t *testing.T) {
	s := ccg.NewState()
	id := s.NewEntity("creature", "0", nil)
	if got := s.AllCounters(id); got != nil {
		t.Fatalf("empty: want nil, got %v", got)
	}
	s.AddCounter(id, "a", 2)
	s.AddCounter(id, "b", 5)
	got := s.AllCounters(id)
	if got["a"] != 2 || got["b"] != 5 {
		t.Fatalf("AllCounters returned wrong values: %+v", got)
	}
	// Mutating the returned map must not affect state.
	got["a"] = 99
	if s.Counters(id, "a") != 2 {
		t.Fatalf("AllCounters did not return a copy")
	}
}

func TestCounterRemovedWhenTotalReachesZero(t *testing.T) {
	s := ccg.NewState()
	id := s.NewEntity("creature", "0", nil)
	s.AddCounter(id, "x", 1)
	s.RemoveCounter(id, "x", 1)
	// AllCounters should be nil (sub-map cleared) or at least not
	// contain "x".
	got := s.AllCounters(id)
	if _, ok := got["x"]; ok {
		t.Fatalf("zero counter should be removed from map: %+v", got)
	}
	// And the reserved attr key should be gone entirely so Entity.Attrs
	// is back to its pre-counter shape.
	e, _ := s.Get(id)
	if _, present := e.Attrs[ccg.CountersAttrKey]; present {
		t.Fatalf("CountersAttrKey should be removed when no counters remain: %+v", e.Attrs)
	}
}

func TestTransferCountersHappyPath(t *testing.T) {
	s := ccg.NewState()
	from := s.NewEntity("creature", "0", nil)
	to := s.NewEntity("creature", "0", nil)
	s.AddCounter(from, "+1/+1", 3)

	var events []ccg.Event
	s.Subscribe(ccg.MatchType(ccg.EventCounterChanged), func(_ *ccg.State, e ccg.Event) {
		events = append(events, e)
	})

	if err := s.TransferCounters(from, to, "+1/+1", 2); err != nil {
		t.Fatalf("transfer: %v", err)
	}
	if s.Counters(from, "+1/+1") != 1 {
		t.Fatalf("from after transfer: want 1, got %d", s.Counters(from, "+1/+1"))
	}
	if s.Counters(to, "+1/+1") != 2 {
		t.Fatalf("to after transfer: want 2, got %d", s.Counters(to, "+1/+1"))
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	// First event is the removal from `from`; second is addition to `to`.
	if events[0].Source != from || events[0].Data["delta"] != -2 {
		t.Fatalf("first event should debit from: %+v", events[0])
	}
	if events[1].Source != to || events[1].Data["delta"] != 2 {
		t.Fatalf("second event should credit to: %+v", events[1])
	}
}

func TestTransferCountersInsufficientFails(t *testing.T) {
	s := ccg.NewState()
	from := s.NewEntity("creature", "0", nil)
	to := s.NewEntity("creature", "0", nil)
	s.AddCounter(from, "x", 1)

	var fires int
	s.Subscribe(ccg.MatchType(ccg.EventCounterChanged), func(_ *ccg.State, _ ccg.Event) {
		fires++
	})

	err := s.TransferCounters(from, to, "x", 5)
	if !errors.Is(err, ccg.ErrInsufficientCounters) {
		t.Fatalf("want ErrInsufficientCounters, got %v", err)
	}
	if s.Counters(from, "x") != 1 || s.Counters(to, "x") != 0 {
		t.Fatalf("state mutated on failed transfer: from=%d to=%d",
			s.Counters(from, "x"), s.Counters(to, "x"))
	}
	if fires != 0 {
		t.Fatalf("no events should fire on failed transfer, got %d", fires)
	}
}

func TestTransferCountersUnknownEntities(t *testing.T) {
	s := ccg.NewState()
	known := s.NewEntity("creature", "0", nil)
	s.AddCounter(known, "x", 1)
	if err := s.TransferCounters(9999, known, "x", 1); !errors.Is(err, ccg.ErrUnknownEntity) {
		t.Fatalf("unknown from: want ErrUnknownEntity, got %v", err)
	}
	if err := s.TransferCounters(known, 9999, "x", 1); !errors.Is(err, ccg.ErrUnknownEntity) {
		t.Fatalf("unknown to: want ErrUnknownEntity, got %v", err)
	}
}

func TestTransferCountersNoOps(t *testing.T) {
	s := ccg.NewState()
	a := s.NewEntity("creature", "0", nil)
	b := s.NewEntity("creature", "0", nil)
	s.AddCounter(a, "x", 3)

	var fires int
	s.Subscribe(ccg.MatchType(ccg.EventCounterChanged), func(_ *ccg.State, _ ccg.Event) {
		fires++
	})

	if err := s.TransferCounters(a, b, "x", 0); err != nil {
		t.Fatalf("zero transfer: %v", err)
	}
	if err := s.TransferCounters(a, b, "x", -1); err != nil {
		t.Fatalf("negative transfer: %v", err)
	}
	if err := s.TransferCounters(a, a, "x", 2); err != nil {
		t.Fatalf("self transfer: %v", err)
	}
	if fires != 0 {
		t.Fatalf("no events should fire on no-op transfers, got %d", fires)
	}
}

func TestCountersRoundTripThroughJSON(t *testing.T) {
	s := ccg.NewState()
	id := s.NewEntity("creature", "0", map[string]any{"power": 2})
	s.AddCounter(id, "+1/+1", 3)
	s.AddCounter(id, "loyalty", 7)

	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got ccg.State
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// Read via Counters — coerces map[string]any back to int.
	if v := got.Counters(id, "+1/+1"); v != 3 {
		t.Fatalf("post-roundtrip Counters(+1/+1): want 3, got %d", v)
	}
	if v := got.Counters(id, "loyalty"); v != 7 {
		t.Fatalf("post-roundtrip Counters(loyalty): want 7, got %d", v)
	}
	// AllCounters returns int-typed copy.
	all := got.AllCounters(id)
	if all["+1/+1"] != 3 || all["loyalty"] != 7 {
		t.Fatalf("post-roundtrip AllCounters: %+v", all)
	}
	// Mutating after roundtrip canonicalises the inner map back to
	// map[string]int and fires a normal event.
	got.AddCounter(id, "+1/+1", 1)
	if v := got.Counters(id, "+1/+1"); v != 4 {
		t.Fatalf("post-roundtrip AddCounter: want 4, got %d", v)
	}
}

func TestCountersDoNotPolluteOtherAttrs(t *testing.T) {
	s := ccg.NewState()
	id := s.NewEntity("creature", "0", map[string]any{"power": 2, "toughness": 2})
	s.AddCounter(id, "+1/+1", 1)

	e, _ := s.Get(id)
	if e.AttrInt("power", 0) != 2 || e.AttrInt("toughness", 0) != 2 {
		t.Fatalf("counter mutations should not touch other Attrs: %+v", e.Attrs)
	}
}

func TestTransitionMovesBatch(t *testing.T) {
	s := ccg.NewState()
	s.NewZone("hand", false)
	s.NewZone("discard", false)
	a := s.NewEntity("card", "0", nil)
	b := s.NewEntity("card", "0", nil)
	c := s.NewEntity("card", "0", nil)
	_ = s.Add("hand", a)
	_ = s.Add("hand", b)
	_ = s.Add("hand", c)

	if err := s.Transition([]ccg.EntityID{a, b, c}, "discard"); err != nil {
		t.Fatalf("transition: %v", err)
	}
	if s.Size("hand") != 0 || s.Size("discard") != 3 {
		t.Fatalf("post-transition: hand=%d discard=%d", s.Size("hand"), s.Size("discard"))
	}
}

func TestTransitionPreFlightZoneFull(t *testing.T) {
	s := ccg.NewState()
	s.NewZone("hand", false)
	z := s.NewZone("battlefield", false)
	z.Capacity = 2
	a := s.NewEntity("creature", "0", nil)
	b := s.NewEntity("creature", "0", nil)
	c := s.NewEntity("creature", "0", nil)
	_ = s.Add("hand", a)
	_ = s.Add("hand", b)
	_ = s.Add("hand", c)

	err := s.Transition([]ccg.EntityID{a, b, c}, "battlefield")
	if !errors.Is(err, ccg.ErrZoneFull) {
		t.Fatalf("want ErrZoneFull, got %v", err)
	}
	// State must be untouched (pre-flight all-or-nothing).
	if s.Size("hand") != 3 || s.Size("battlefield") != 0 {
		t.Fatalf("partial mutation on rejected transition: hand=%d battlefield=%d",
			s.Size("hand"), s.Size("battlefield"))
	}
}

func TestTransitionPreFlightUnknownEntity(t *testing.T) {
	s := ccg.NewState()
	s.NewZone("hand", false)
	s.NewZone("discard", false)
	a := s.NewEntity("card", "0", nil)
	_ = s.Add("hand", a)

	err := s.Transition([]ccg.EntityID{a, ccg.EntityID(9999)}, "discard")
	if !errors.Is(err, ccg.ErrUnknownEntity) {
		t.Fatalf("want ErrUnknownEntity, got %v", err)
	}
	if s.Size("hand") != 1 || s.Size("discard") != 0 {
		t.Fatalf("partial mutation on rejected transition")
	}
}

func TestTransitionPreFlightUnknownZone(t *testing.T) {
	s := ccg.NewState()
	if err := s.Transition(nil, "missing"); !errors.Is(err, ccg.ErrUnknownZone) {
		t.Fatalf("want ErrUnknownZone, got %v", err)
	}
}

func TestTransitionIgnoresAlreadyInTarget(t *testing.T) {
	// ids already in `to` shouldn't count for capacity — moving them
	// is effectively a no-op.
	s := ccg.NewState()
	z := s.NewZone("lane", false)
	z.Capacity = 2
	a := s.NewEntity("creature", "0", nil)
	b := s.NewEntity("creature", "0", nil)
	_ = s.Add("lane", a)
	_ = s.Add("lane", b)
	// Both ids are already in `lane`; capacity 2; transition should succeed.
	if err := s.Transition([]ccg.EntityID{a, b}, "lane"); err != nil {
		t.Fatalf("self-targeting transition: %v", err)
	}
}

func TestMoveAllToSweepsSources(t *testing.T) {
	s := ccg.NewState()
	s.NewZone("battlefield", false)
	s.NewZone("aura", false)
	s.NewZone("graveyard", false)
	for i := 0; i < 3; i++ {
		_ = s.Add("battlefield", s.NewEntity("creature", "0", nil))
	}
	for i := 0; i < 2; i++ {
		_ = s.Add("aura", s.NewEntity("enchantment", "0", nil))
	}

	if err := s.MoveAllTo("graveyard", "battlefield", "aura"); err != nil {
		t.Fatalf("MoveAllTo: %v", err)
	}
	if s.Size("battlefield") != 0 || s.Size("aura") != 0 || s.Size("graveyard") != 5 {
		t.Fatalf("post-sweep: bf=%d aura=%d gy=%d",
			s.Size("battlefield"), s.Size("aura"), s.Size("graveyard"))
	}
}

func TestMoveAllToPreFlightCapacity(t *testing.T) {
	s := ccg.NewState()
	s.NewZone("hand", false)
	z := s.NewZone("discard", false)
	z.Capacity = 2
	for i := 0; i < 3; i++ {
		_ = s.Add("hand", s.NewEntity("card", "0", nil))
	}
	err := s.MoveAllTo("discard", "hand")
	if !errors.Is(err, ccg.ErrZoneFull) {
		t.Fatalf("want ErrZoneFull, got %v", err)
	}
	if s.Size("hand") != 3 || s.Size("discard") != 0 {
		t.Fatalf("partial sweep on rejected MoveAllTo: hand=%d discard=%d",
			s.Size("hand"), s.Size("discard"))
	}
}

func TestMoveAllToUnknownZones(t *testing.T) {
	s := ccg.NewState()
	s.NewZone("hand", false)
	if err := s.MoveAllTo("missing", "hand"); !errors.Is(err, ccg.ErrUnknownZone) {
		t.Fatalf("unknown target: want ErrUnknownZone, got %v", err)
	}
	if err := s.MoveAllTo("hand", "missing"); !errors.Is(err, ccg.ErrUnknownZone) {
		t.Fatalf("unknown source: want ErrUnknownZone, got %v", err)
	}
}

func TestMoveAllToSelfSourceSkipped(t *testing.T) {
	s := ccg.NewState()
	z := s.NewZone("lane", false)
	z.Capacity = 1
	_ = s.Add("lane", s.NewEntity("c", "0", nil))
	// Source equals target — capacity check should ignore those entities.
	if err := s.MoveAllTo("lane", "lane"); err != nil {
		t.Fatalf("self-source MoveAllTo should be a no-op: %v", err)
	}
}

func TestTopAndBottom(t *testing.T) {
	s := ccg.NewState()
	s.NewZone("deck", true)
	if _, ok := s.Top("deck"); ok {
		t.Fatalf("empty zone Top should return (_, false)")
	}
	if _, ok := s.Bottom("deck"); ok {
		t.Fatalf("empty zone Bottom should return (_, false)")
	}
	if _, ok := s.Top("missing"); ok {
		t.Fatalf("unknown zone Top should return (_, false)")
	}
	a := s.NewEntity("card", "0", nil)
	b := s.NewEntity("card", "0", nil)
	c := s.NewEntity("card", "0", nil)
	_ = s.Add("deck", a) // bottom
	_ = s.Add("deck", b)
	_ = s.Add("deck", c) // top
	top, ok := s.Top("deck")
	if !ok || top != c {
		t.Fatalf("Top: want %d, got %d (ok=%v)", c, top, ok)
	}
	bot, ok := s.Bottom("deck")
	if !ok || bot != a {
		t.Fatalf("Bottom: want %d, got %d (ok=%v)", a, bot, ok)
	}
}

func TestMulliganReshufflesAndRedraws(t *testing.T) {
	s := ccg.NewState()
	s.NewZone("hand", false)
	s.NewZone("deck", true)
	var hand []ccg.EntityID
	for i := 0; i < 3; i++ {
		id := s.NewEntity("card", "0", nil)
		_ = s.Add("hand", id)
		hand = append(hand, id)
	}
	for i := 0; i < 5; i++ {
		_ = s.Add("deck", s.NewEntity("card", "0", nil))
	}

	drawn, err := s.Mulligan("hand", "deck", 3, testRandom(42))
	if err != nil {
		t.Fatalf("mulligan: %v", err)
	}
	if len(drawn) != 3 {
		t.Fatalf("drawn count: want 3, got %d", len(drawn))
	}
	if s.Size("hand") != 3 {
		t.Fatalf("hand size: want 3, got %d", s.Size("hand"))
	}
	// Deck previously had 5 + 3 (from hand) = 8; we drew 3, so deck = 5.
	if s.Size("deck") != 5 {
		t.Fatalf("deck size: want 5, got %d", s.Size("deck"))
	}
	// All drawn ids must now be in hand.
	for _, id := range drawn {
		if !s.Contains("hand", id) {
			t.Fatalf("drawn id %d not in hand", id)
		}
	}
}

func TestMulliganZeroJustReshuffles(t *testing.T) {
	s := ccg.NewState()
	s.NewZone("hand", false)
	s.NewZone("deck", true)
	for i := 0; i < 3; i++ {
		_ = s.Add("hand", s.NewEntity("card", "0", nil))
	}
	for i := 0; i < 5; i++ {
		_ = s.Add("deck", s.NewEntity("card", "0", nil))
	}
	drawn, err := s.Mulligan("hand", "deck", 0, testRandom(1))
	if err != nil {
		t.Fatalf("mulligan(0): %v", err)
	}
	if len(drawn) != 0 {
		t.Fatalf("drawn: want 0, got %d", len(drawn))
	}
	if s.Size("hand") != 0 || s.Size("deck") != 8 {
		t.Fatalf("mulligan(0) should empty hand into deck: hand=%d deck=%d",
			s.Size("hand"), s.Size("deck"))
	}
}

func TestMulliganDeterministic(t *testing.T) {
	build := func() *ccg.State {
		s := ccg.NewState()
		s.NewZone("hand", false)
		s.NewZone("deck", true)
		for i := 0; i < 3; i++ {
			_ = s.Add("hand", s.NewEntity("card", "0", nil))
		}
		for i := 0; i < 5; i++ {
			_ = s.Add("deck", s.NewEntity("card", "0", nil))
		}
		return s
	}
	a, _ := build().Mulligan("hand", "deck", 3, testRandom(99))
	b, _ := build().Mulligan("hand", "deck", 3, testRandom(99))
	if len(a) != len(b) {
		t.Fatalf("Mulligan length mismatch")
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("Mulligan not deterministic at %d: %v vs %v", i, a, b)
		}
	}
}

func TestDealInstantiatesPerPlayerPerDef(t *testing.T) {
	c := ccg.NewCatalog()
	_ = c.Register(ccg.CardDef{ID: "x", Type: "creature"})
	_ = c.Register(ccg.CardDef{ID: "y", Type: "spell"})

	s := ccg.NewState()
	s.NewZone("hand:0", false)
	s.NewZone("hand:1", false)

	defs := []ccg.DefID{"x", "y", "x"}
	ids, err := s.Deal(c, defs, []string{"0", "1"}, func(p string) ccg.ZoneName {
		return ccg.ZoneName("hand:" + p)
	})
	if err != nil {
		t.Fatalf("deal: %v", err)
	}
	if len(ids) != 6 {
		t.Fatalf("expected 6 entities, got %d", len(ids))
	}
	// First 3 belong to player 0 in defs order.
	for i := 0; i < 3; i++ {
		e, _ := s.Get(ids[i])
		if e.Owner != "0" || e.DefID != defs[i] {
			t.Fatalf("player-0 slot %d: owner=%q def=%q want owner=0 def=%s",
				i, e.Owner, e.DefID, defs[i])
		}
	}
	for i := 0; i < 3; i++ {
		e, _ := s.Get(ids[3+i])
		if e.Owner != "1" || e.DefID != defs[i] {
			t.Fatalf("player-1 slot %d: owner=%q def=%q want owner=1 def=%s",
				i, e.Owner, e.DefID, defs[i])
		}
	}
	if s.Size("hand:0") != 3 || s.Size("hand:1") != 3 {
		t.Fatalf("hand sizes: 0=%d 1=%d", s.Size("hand:0"), s.Size("hand:1"))
	}
}

func TestDealUnknownDefStopsEarly(t *testing.T) {
	c := ccg.NewCatalog()
	_ = c.Register(ccg.CardDef{ID: "x"})

	s := ccg.NewState()
	s.NewZone("hand:0", false)
	_, err := s.Deal(c, []ccg.DefID{"x", "ghost"}, []string{"0"}, func(p string) ccg.ZoneName {
		return ccg.ZoneName("hand:" + p)
	})
	if !errors.Is(err, ccg.ErrUnknownDef) {
		t.Fatalf("want ErrUnknownDef, got %v", err)
	}
	// The first card ("x") was dealt before the failure on "ghost".
	if s.Size("hand:0") != 1 {
		t.Fatalf("partial deal: hand:0=%d (expected 1 from the pre-failure card)", s.Size("hand:0"))
	}
}

func TestNewDeckPileEnsuresZones(t *testing.T) {
	s := ccg.NewState()
	pile := s.NewDeckPile("d", "g")
	if pile.DeckZone != "d" || pile.DiscardZone != "g" {
		t.Fatalf("DeckPile labels: %+v", pile)
	}
	if _, ok := s.Zones["d"]; !ok {
		t.Fatalf("deck zone not created")
	}
	if _, ok := s.Zones["g"]; !ok {
		t.Fatalf("discard zone not created")
	}
	pile2 := s.NewDeckPile("d", "g")
	if pile2.DeckZone != pile.DeckZone || pile2.DiscardZone != pile.DiscardZone {
		t.Fatalf("re-call should return matching labels")
	}
}

func TestDeckPileDrawAndDiscard(t *testing.T) {
	s := ccg.NewState()
	pile := s.NewDeckPile("deck", "discard")
	for i := 0; i < 5; i++ {
		_ = s.Add("deck", s.NewEntity("card", "0", nil))
	}
	drawn, err := pile.Draw(s, 2, testRandom(1))
	if err != nil {
		t.Fatalf("draw: %v", err)
	}
	if len(drawn) != 2 {
		t.Fatalf("draw count: want 2, got %d", len(drawn))
	}
	if pile.DeckSize(s) != 3 {
		t.Fatalf("deck size after draw: want 3, got %d", pile.DeckSize(s))
	}
	if err := pile.Discard(s, drawn...); err != nil {
		t.Fatalf("discard: %v", err)
	}
	if pile.DiscardSize(s) != 2 {
		t.Fatalf("discard size: want 2, got %d", pile.DiscardSize(s))
	}
}

func TestDeckPileDrawEmptyNoAutoReshuffle(t *testing.T) {
	s := ccg.NewState()
	pile := s.NewDeckPile("deck", "discard")
	_ = s.Add("discard", s.NewEntity("card", "0", nil))
	_, err := pile.Draw(s, 1, testRandom(1))
	if !errors.Is(err, ccg.ErrZoneEmpty) {
		t.Fatalf("want ErrZoneEmpty, got %v", err)
	}
	if pile.DeckSize(s) != 0 || pile.DiscardSize(s) != 1 {
		t.Fatalf("default policy should not auto-reshuffle: deck=%d discard=%d",
			pile.DeckSize(s), pile.DiscardSize(s))
	}
}

func TestDeckPileDrawAutoReshuffle(t *testing.T) {
	s := ccg.NewState()
	pile := s.NewDeckPile("deck", "discard")
	pile.AutoReshuffleOnEmpty = true
	for i := 0; i < 3; i++ {
		_ = s.Add("discard", s.NewEntity("card", "0", nil))
	}
	drawn, err := pile.Draw(s, 2, testRandom(7))
	if err != nil {
		t.Fatalf("auto-reshuffled draw: %v", err)
	}
	if len(drawn) != 2 {
		t.Fatalf("drawn count: want 2, got %d", len(drawn))
	}
	if pile.DeckSize(s) != 1 || pile.DiscardSize(s) != 0 {
		t.Fatalf("after reshuffle+draw 2: deck=%d discard=%d (want 1 / 0)",
			pile.DeckSize(s), pile.DiscardSize(s))
	}
}

func TestDeckPileMillUsesReshuffle(t *testing.T) {
	s := ccg.NewState()
	pile := s.NewDeckPile("deck", "discard")
	pile.AutoReshuffleOnEmpty = true
	for i := 0; i < 2; i++ {
		_ = s.Add("deck", s.NewEntity("card", "0", nil))
	}
	milled, err := pile.Mill(s, 2, testRandom(1))
	if err != nil {
		t.Fatalf("mill: %v", err)
	}
	if len(milled) != 2 {
		t.Fatalf("milled count: %d", len(milled))
	}
	for _, id := range milled {
		if !s.Contains("discard", id) {
			t.Fatalf("milled %d not in discard", id)
		}
	}
}

func TestDeckPileReshuffleDiscardIntoDeck(t *testing.T) {
	s := ccg.NewState()
	pile := s.NewDeckPile("deck", "discard")
	for i := 0; i < 4; i++ {
		_ = s.Add("discard", s.NewEntity("card", "0", nil))
	}
	if err := pile.ReshuffleDiscardIntoDeck(s, testRandom(1)); err != nil {
		t.Fatalf("reshuffle: %v", err)
	}
	if pile.DeckSize(s) != 4 || pile.DiscardSize(s) != 0 {
		t.Fatalf("post-reshuffle: deck=%d discard=%d (want 4 / 0)",
			pile.DeckSize(s), pile.DiscardSize(s))
	}
}

func TestDeckPileReshuffleEmptyIsNoOp(t *testing.T) {
	s := ccg.NewState()
	pile := s.NewDeckPile("deck", "discard")
	if err := pile.ReshuffleDiscardIntoDeck(s, testRandom(1)); err != nil {
		t.Fatalf("empty reshuffle should be no-op: %v", err)
	}
}

func TestDeckPileDeterministic(t *testing.T) {
	build := func() (*ccg.State, *ccg.DeckPile) {
		s := ccg.NewState()
		pile := s.NewDeckPile("deck", "discard")
		pile.AutoReshuffleOnEmpty = true
		for i := 0; i < 4; i++ {
			_ = s.Add("discard", s.NewEntity("card", "0", nil))
		}
		return s, pile
	}
	s1, p1 := build()
	a, _ := p1.Draw(s1, 3, testRandom(99))
	s2, p2 := build()
	b, _ := p2.Draw(s2, 3, testRandom(99))
	if len(a) != len(b) {
		t.Fatalf("draw length mismatch")
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("pile.Draw not deterministic at %d: %v vs %v", i, a, b)
		}
	}
}

func TestDeckPileDiscardPropagatesCapacityError(t *testing.T) {
	s := ccg.NewState()
	pile := s.NewDeckPile("deck", "discard")
	s.Zones["discard"].Capacity = 1
	a := s.NewEntity("card", "0", nil)
	b := s.NewEntity("card", "0", nil)
	_ = s.Add("deck", a)
	_ = s.Add("deck", b)
	if err := pile.Discard(s, a, b); !errors.Is(err, ccg.ErrZoneFull) {
		t.Fatalf("want ErrZoneFull, got %v", err)
	}
	if pile.DeckSize(s) != 2 {
		t.Fatalf("deck partially emptied on rejected discard: %d", pile.DeckSize(s))
	}
}

func TestBindAbilityFiresWhileInZone(t *testing.T) {
	s := ccg.NewState()
	s.NewZone("battlefield", false)
	id := s.NewEntity("creature", "0", nil)
	_ = s.Add("battlefield", id)

	var fires int
	s.BindAbility(id, []ccg.ZoneName{"battlefield"},
		ccg.MatchType("ping"),
		func(_ *ccg.State, _ ccg.Event) { fires++ },
	)
	s.Publish(ccg.Event{Type: "ping"})
	s.Publish(ccg.Event{Type: "ping"})
	if fires != 2 {
		t.Fatalf("expected 2 fires while in zone, got %d", fires)
	}
}

func TestBindAbilityAutoUnbindsOnZoneLeave(t *testing.T) {
	s := ccg.NewState()
	s.NewZone("battlefield", false)
	s.NewZone("graveyard", false)
	id := s.NewEntity("creature", "0", nil)
	_ = s.Add("battlefield", id)

	var fires int
	s.BindAbility(id, []ccg.ZoneName{"battlefield"},
		ccg.MatchType("ping"),
		func(_ *ccg.State, _ ccg.Event) { fires++ },
	)
	s.Publish(ccg.Event{Type: "ping"})
	_ = s.MoveTo(id, "graveyard")
	s.Publish(ccg.Event{Type: "ping"})
	s.Publish(ccg.Event{Type: "ping"})

	if fires != 1 {
		t.Fatalf("expected 1 fire before zone-out, got %d", fires)
	}
	// Re-entry does NOT auto-rebind.
	_ = s.MoveTo(id, "battlefield")
	s.Publish(ccg.Event{Type: "ping"})
	if fires != 1 {
		t.Fatalf("re-entry should not auto-rebind, got %d fires", fires)
	}
}

func TestBindAbilityAutoUnbindsOnDestroy(t *testing.T) {
	s := ccg.NewState()
	s.NewZone("battlefield", false)
	id := s.NewEntity("creature", "0", nil)
	_ = s.Add("battlefield", id)

	var fires int
	s.BindAbility(id, []ccg.ZoneName{"battlefield"},
		ccg.MatchType("ping"),
		func(_ *ccg.State, _ ccg.Event) { fires++ },
	)
	s.Destroy(id)
	s.Publish(ccg.Event{Type: "ping"})
	if fires != 0 {
		t.Fatalf("expected 0 fires after destroy, got %d", fires)
	}
}

func TestBindAbilityMultiZone(t *testing.T) {
	s := ccg.NewState()
	s.NewZone("battlefield", false)
	s.NewZone("graveyard", false)
	s.NewZone("exile", false)
	id := s.NewEntity("creature", "0", nil)
	_ = s.Add("battlefield", id)

	var fires int
	s.BindAbility(id, []ccg.ZoneName{"battlefield", "graveyard"},
		ccg.MatchType("ping"),
		func(_ *ccg.State, _ ccg.Event) { fires++ },
	)
	s.Publish(ccg.Event{Type: "ping"})
	_ = s.MoveTo(id, "graveyard")
	s.Publish(ccg.Event{Type: "ping"})
	_ = s.MoveTo(id, "exile")
	s.Publish(ccg.Event{Type: "ping"})
	if fires != 2 {
		t.Fatalf("multi-zone: want 2 fires, got %d", fires)
	}
}

func TestBindAbilityExplicitUnbind(t *testing.T) {
	s := ccg.NewState()
	s.NewZone("battlefield", false)
	id := s.NewEntity("creature", "0", nil)
	_ = s.Add("battlefield", id)

	var fires int
	aid := s.BindAbility(id, []ccg.ZoneName{"battlefield"},
		ccg.MatchType("ping"),
		func(_ *ccg.State, _ ccg.Event) { fires++ },
	)
	s.Publish(ccg.Event{Type: "ping"})
	s.UnbindAbility(aid)
	s.Publish(ccg.Event{Type: "ping"})
	if fires != 1 {
		t.Fatalf("expected 1 fire before explicit unbind, got %d", fires)
	}
	// Re-unbinding is a no-op; unknown ids too.
	s.UnbindAbility(aid)
	s.UnbindAbility(ccg.AbilityID(9999))
}

func TestBindAbilityIndependentBindingsPerEntity(t *testing.T) {
	s := ccg.NewState()
	s.NewZone("battlefield", false)
	s.NewZone("graveyard", false)
	a := s.NewEntity("creature", "0", nil)
	b := s.NewEntity("creature", "0", nil)
	_ = s.Add("battlefield", a)
	_ = s.Add("battlefield", b)

	var aFires, bFires int
	s.BindAbility(a, []ccg.ZoneName{"battlefield"},
		ccg.MatchType("ping"),
		func(_ *ccg.State, _ ccg.Event) { aFires++ },
	)
	s.BindAbility(b, []ccg.ZoneName{"battlefield"},
		ccg.MatchType("ping"),
		func(_ *ccg.State, _ ccg.Event) { bFires++ },
	)
	s.Publish(ccg.Event{Type: "ping"})
	_ = s.MoveTo(a, "graveyard")
	s.Publish(ccg.Event{Type: "ping"})

	if aFires != 1 {
		t.Fatalf("aFires: want 1, got %d", aFires)
	}
	if bFires != 2 {
		t.Fatalf("bFires: want 2, got %d", bFires)
	}
}

func TestBindAbilityMultipleBindingsOnSameEntity(t *testing.T) {
	s := ccg.NewState()
	s.NewZone("battlefield", false)
	id := s.NewEntity("creature", "0", nil)
	_ = s.Add("battlefield", id)

	var pingFires, summonFires int
	s.BindAbility(id, []ccg.ZoneName{"battlefield"},
		ccg.MatchType("ping"),
		func(_ *ccg.State, _ ccg.Event) { pingFires++ },
	)
	s.BindAbility(id, []ccg.ZoneName{"battlefield"},
		ccg.MatchType("summon"),
		func(_ *ccg.State, _ ccg.Event) { summonFires++ },
	)
	s.Publish(ccg.Event{Type: "ping"})
	s.Publish(ccg.Event{Type: "summon"})
	s.Publish(ccg.Event{Type: "unrelated"})

	if pingFires != 1 || summonFires != 1 {
		t.Fatalf("independent triggers: ping=%d summon=%d", pingFires, summonFires)
	}
}

func TestBindAbilityIgnoresNonMatchingPredicate(t *testing.T) {
	s := ccg.NewState()
	s.NewZone("battlefield", false)
	id := s.NewEntity("creature", "0", nil)
	_ = s.Add("battlefield", id)

	var fires int
	s.BindAbility(id, []ccg.ZoneName{"battlefield"},
		ccg.MatchType("ping"),
		func(_ *ccg.State, _ ccg.Event) { fires++ },
	)
	s.Publish(ccg.Event{Type: "irrelevant"})
	if fires != 0 {
		t.Fatalf("non-matching predicate fired anyway: %d", fires)
	}
}

func TestBindAbilityBoundZonesDefensiveCopy(t *testing.T) {
	s := ccg.NewState()
	s.NewZone("battlefield", false)
	s.NewZone("graveyard", false)
	id := s.NewEntity("creature", "0", nil)
	_ = s.Add("battlefield", id)

	zones := []ccg.ZoneName{"battlefield"}
	var fires int
	s.BindAbility(id, zones, ccg.MatchType("ping"),
		func(_ *ccg.State, _ ccg.Event) { fires++ },
	)
	zones[0] = "graveyard" // caller mutates after binding
	s.Publish(ccg.Event{Type: "ping"})
	if fires != 1 {
		t.Fatalf("defensive-copy failed; got %d fires", fires)
	}
}
