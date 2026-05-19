package ccg_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/tjcran/boardgame-go/modules/ccg"
)

// damageResolver is a tiny resolver used by several tests: applies
// eff.Data["amount"] (int) HP loss to a per-test HP map keyed by
// EntityID.
func damageResolver(hp map[ccg.EntityID]int) ccg.Resolver {
	return func(_ *ccg.State, eff ccg.Effect) error {
		amt, _ := eff.Data["amount"].(int)
		hp[eff.Target] -= amt
		return nil
	}
}

// TestPushEffectAssignsMonotonicIDs verifies stable, monotonic ID
// assignment across mixed Push / PushFront / Insert.
func TestPushEffectAssignsMonotonicIDs(t *testing.T) {
	s := ccg.NewState()
	id1 := s.PushEffect(ccg.Effect{Kind: "damage"})
	id2 := s.PushEffectFront(ccg.Effect{Kind: "draw"})
	id3 := s.InsertEffectAt(1, ccg.Effect{Kind: "scry"})
	if id1 != 1 || id2 != 2 || id3 != 3 {
		t.Fatalf("expected IDs 1,2,3 in push order, got %d,%d,%d", id1, id2, id3)
	}
	if len(s.PendingEffects) != 3 {
		t.Fatalf("expected 3 pending, got %d", len(s.PendingEffects))
	}
	// Order: front=draw (id2), middle=scry (id3), back=damage (id1).
	if s.PendingEffects[0].ID != id2 ||
		s.PendingEffects[1].ID != id3 ||
		s.PendingEffects[2].ID != id1 {
		t.Fatalf("unexpected order: %+v", s.PendingEffects)
	}
}

// TestResolveNextFIFO drains the queue front-to-back using PickFront.
func TestResolveNextFIFO(t *testing.T) {
	s := ccg.NewState()
	hp := map[ccg.EntityID]int{10: 5, 20: 5}
	table := ccg.ResolverTable{"damage": damageResolver(hp)}

	s.PushEffect(ccg.Effect{Kind: "damage", Target: 10, Data: map[string]any{"amount": 1}})
	s.PushEffect(ccg.Effect{Kind: "damage", Target: 20, Data: map[string]any{"amount": 2}})
	s.PushEffect(ccg.Effect{Kind: "damage", Target: 10, Data: map[string]any{"amount": 3}})

	for {
		_, ok, err := s.ResolveNext(ccg.PickFront, table)
		if err != nil {
			t.Fatalf("ResolveNext: %v", err)
		}
		if !ok {
			break
		}
	}
	if hp[10] != 1 || hp[20] != 3 {
		t.Fatalf("expected HP{10:1, 20:3}, got %+v", hp)
	}
	if len(s.PendingEffects) != 0 {
		t.Fatalf("expected drained queue, got %d", len(s.PendingEffects))
	}
}

// TestResolveNextLIFO drains back-to-front using PickBack. The same
// pushed effects resolve in reverse order; this proves the library is
// neutral on resolution order — only the Picker differs.
func TestResolveNextLIFO(t *testing.T) {
	s := ccg.NewState()
	order := []ccg.EntityID{}
	table := ccg.ResolverTable{
		"mark": func(_ *ccg.State, eff ccg.Effect) error {
			order = append(order, eff.Target)
			return nil
		},
	}
	s.PushEffect(ccg.Effect{Kind: "mark", Target: 1})
	s.PushEffect(ccg.Effect{Kind: "mark", Target: 2})
	s.PushEffect(ccg.Effect{Kind: "mark", Target: 3})

	for {
		_, ok, err := s.ResolveNext(ccg.PickBack, table)
		if err != nil {
			t.Fatalf("ResolveNext: %v", err)
		}
		if !ok {
			break
		}
	}
	want := []ccg.EntityID{3, 2, 1}
	if len(order) != 3 || order[0] != want[0] || order[1] != want[1] || order[2] != want[2] {
		t.Fatalf("expected LIFO order %v, got %v", want, order)
	}
}

// TestResolveNextHonorsCountered verifies Countered effects skip the
// resolver but still drain and publish effect_resolved.
func TestResolveNextHonorsCountered(t *testing.T) {
	s := ccg.NewState()
	called := false
	table := ccg.ResolverTable{
		"damage": func(_ *ccg.State, _ ccg.Effect) error {
			called = true
			return nil
		},
	}
	id := s.PushEffect(ccg.Effect{Kind: "damage", Target: 1, Countered: true})

	eff, ok, err := s.ResolveNext(ccg.PickFront, table)
	if err != nil || !ok {
		t.Fatalf("expected resolved=true err=nil, got ok=%v err=%v", ok, err)
	}
	if called {
		t.Fatalf("resolver should not have fired for countered effect")
	}
	if eff.ID != id {
		t.Fatalf("expected resolved id=%d, got %d", id, eff.ID)
	}
	if len(s.PendingEffects) != 0 {
		t.Fatalf("countered effect should still drain off the queue")
	}
}

// TestReplaceEffectPreservesIndex demonstrates the replacement-effect
// pattern: a subscriber on effect_pushed swaps the payload while
// keeping the queue position.
func TestReplaceEffectPreservesIndex(t *testing.T) {
	s := ccg.NewState()
	s.PushEffect(ccg.Effect{Kind: "first"})
	id := s.PushEffect(ccg.Effect{Kind: "draw", Data: map[string]any{"count": 1}})
	s.PushEffect(ccg.Effect{Kind: "last"})

	ok := s.ReplaceEffect(id, ccg.Effect{Kind: "draw", Data: map[string]any{"count": 2}})
	if !ok {
		t.Fatalf("ReplaceEffect returned false for known id")
	}
	if got, _, _ := s.FindEffect(id); got.Data["count"] != 2 {
		t.Fatalf("expected count=2 after replacement, got %v", got.Data["count"])
	}
	// Index must be unchanged so neighbouring effects keep their order.
	if s.PendingEffects[1].ID != id {
		t.Fatalf("expected replaced effect still at index 1, queue=%+v", s.PendingEffects)
	}
}

// TestRemoveEffectFizzlesPending verifies counterspell flow:
// RemoveEffect drops the entry and publishes effect_removed.
func TestRemoveEffectFizzlesPending(t *testing.T) {
	s := ccg.NewState()
	id := s.PushEffect(ccg.Effect{Kind: "damage", Source: 5})

	var sawRemoved bool
	s.Subscribe(ccg.MatchType(ccg.EventEffectRemoved), func(_ *ccg.State, ev ccg.Event) {
		if ev.Data["effect_id"].(ccg.EffectID) == id {
			sawRemoved = true
		}
	})

	if ok := s.RemoveEffect(id); !ok {
		t.Fatalf("RemoveEffect returned false for known id")
	}
	if !sawRemoved {
		t.Fatalf("expected effect_removed subscriber to fire")
	}
	if len(s.PendingEffects) != 0 {
		t.Fatalf("expected queue empty after RemoveEffect")
	}
	// Idempotent: removing an unknown id is a no-op false return.
	if s.RemoveEffect(id) {
		t.Fatalf("second RemoveEffect on same id should return false")
	}
}

// TestRedirectionViaResolvingSubscriber is the worked example for
// redirect effects. A subscriber on effect_resolving mutates the
// pending effect's Target before the resolver runs; the resolver sees
// the new target.
func TestRedirectionViaResolvingSubscriber(t *testing.T) {
	s := ccg.NewState()
	hp := map[ccg.EntityID]int{10: 5, 20: 5}
	table := ccg.ResolverTable{"damage": damageResolver(hp)}

	// Static ability: any damage aimed at 10 is redirected to 20.
	s.Subscribe(ccg.MatchType(ccg.EventEffectResolving), func(st *ccg.State, ev ccg.Event) {
		id := ev.Data["effect_id"].(ccg.EffectID)
		st.MutateEffect(id, func(e *ccg.Effect) {
			if e.Kind == "damage" && e.Target == 10 {
				e.Target = 20
			}
		})
	})

	s.PushEffect(ccg.Effect{Kind: "damage", Target: 10, Data: map[string]any{"amount": 3}})
	_, ok, err := s.ResolveNext(ccg.PickFront, table)
	if err != nil || !ok {
		t.Fatalf("ResolveNext: ok=%v err=%v", ok, err)
	}
	if hp[10] != 5 {
		t.Fatalf("expected target 10 unharmed after redirect, got %d", hp[10])
	}
	if hp[20] != 2 {
		t.Fatalf("expected target 20 to absorb redirected damage, got %d", hp[20])
	}
}

// TestCounterspellViaResolvingSubscriber demonstrates the
// counterspell pattern: a handler on effect_resolving calls
// RemoveEffect to cancel resolution mid-flight.
func TestCounterspellViaResolvingSubscriber(t *testing.T) {
	s := ccg.NewState()
	hp := map[ccg.EntityID]int{10: 5}
	called := false
	table := ccg.ResolverTable{
		"damage": func(_ *ccg.State, _ ccg.Effect) error {
			called = true
			hp[10]--
			return nil
		},
	}

	// "Counter all damage" trigger.
	s.Subscribe(ccg.MatchType(ccg.EventEffectResolving), func(st *ccg.State, ev ccg.Event) {
		id := ev.Data["effect_id"].(ccg.EffectID)
		eff, _, _ := st.FindEffect(id)
		if eff.Kind == "damage" {
			st.RemoveEffect(id)
		}
	})

	s.PushEffect(ccg.Effect{Kind: "damage", Target: 10, Data: map[string]any{"amount": 1}})
	eff, ok, err := s.ResolveNext(ccg.PickFront, table)
	if err != nil {
		t.Fatalf("ResolveNext: %v", err)
	}
	if ok {
		t.Fatalf("expected ResolveNext to return ok=false when handler removed effect, got effect=%+v", eff)
	}
	if called {
		t.Fatalf("resolver should not have fired for countered effect")
	}
	if hp[10] != 5 {
		t.Fatalf("target should be untouched, got hp=%d", hp[10])
	}
}

// TestLifecycleEventsCarryEffectID verifies every lifecycle event
// publishes Event.Data["effect_id"] with the correct EffectID. This
// is the contract subscribers rely on to act on the queue.
func TestLifecycleEventsCarryEffectID(t *testing.T) {
	s := ccg.NewState()
	seen := map[string]ccg.EffectID{}
	for _, evType := range []string{
		ccg.EventEffectPushed,
		ccg.EventEffectResolving,
		ccg.EventEffectResolved,
		ccg.EventEffectRemoved,
		ccg.EventEffectReplaced,
	} {
		et := evType
		s.Subscribe(ccg.MatchType(et), func(_ *ccg.State, ev ccg.Event) {
			seen[et] = ev.Data["effect_id"].(ccg.EffectID)
		})
	}

	// Push -> replace -> push another -> resolve first -> remove second.
	id1 := s.PushEffect(ccg.Effect{Kind: "a"})
	s.ReplaceEffect(id1, ccg.Effect{Kind: "a2"})
	id2 := s.PushEffect(ccg.Effect{Kind: "b"})
	table := ccg.ResolverTable{
		"a2": func(_ *ccg.State, _ ccg.Effect) error { return nil },
		"b":  func(_ *ccg.State, _ ccg.Effect) error { return nil },
	}
	if _, ok, err := s.ResolveNext(ccg.PickFront, table); err != nil || !ok {
		t.Fatalf("resolve id1: ok=%v err=%v", ok, err)
	}
	s.RemoveEffect(id2)

	if seen[ccg.EventEffectPushed] == 0 {
		t.Fatalf("missing effect_pushed event")
	}
	if seen[ccg.EventEffectReplaced] != id1 {
		t.Fatalf("expected effect_replaced for id1=%d, got %d", id1, seen[ccg.EventEffectReplaced])
	}
	if seen[ccg.EventEffectResolving] != id1 {
		t.Fatalf("expected effect_resolving for id1=%d, got %d", id1, seen[ccg.EventEffectResolving])
	}
	if seen[ccg.EventEffectResolved] != id1 {
		t.Fatalf("expected effect_resolved for id1=%d, got %d", id1, seen[ccg.EventEffectResolved])
	}
	if seen[ccg.EventEffectRemoved] != id2 {
		t.Fatalf("expected effect_removed for id2=%d, got %d", id2, seen[ccg.EventEffectRemoved])
	}
}

// TestUnknownKindReturnsError verifies the missing-resolver guard.
// The effect stays on the queue so the caller can inspect.
func TestUnknownKindReturnsError(t *testing.T) {
	s := ccg.NewState()
	id := s.PushEffect(ccg.Effect{Kind: "mystery"})
	_, ok, err := s.ResolveNext(ccg.PickFront, ccg.ResolverTable{})
	if ok {
		t.Fatalf("expected ok=false on unknown kind")
	}
	var ukErr *ccg.UnknownEffectKindError
	if !errors.As(err, &ukErr) {
		t.Fatalf("expected UnknownEffectKindError, got %T %v", err, err)
	}
	if ukErr.Kind != "mystery" {
		t.Fatalf("expected Kind=mystery, got %q", ukErr.Kind)
	}
	if _, _, ok := s.FindEffect(id); !ok {
		t.Fatalf("effect should remain pending after unknown-kind error")
	}
}

// TestResolverErrorLeavesEffectPending verifies a resolver returning
// an error halts ResolveNext and leaves the effect on the queue.
func TestResolverErrorLeavesEffectPending(t *testing.T) {
	s := ccg.NewState()
	id := s.PushEffect(ccg.Effect{Kind: "boom"})
	boom := errors.New("kaboom")
	_, ok, err := s.ResolveNext(ccg.PickFront, ccg.ResolverTable{
		"boom": func(_ *ccg.State, _ ccg.Effect) error { return boom },
	})
	if ok || !errors.Is(err, boom) {
		t.Fatalf("expected ok=false err=boom, got ok=%v err=%v", ok, err)
	}
	if _, _, found := s.FindEffect(id); !found {
		t.Fatalf("effect should remain pending after resolver error")
	}
}

// TestInsertEffectAtClampsRange covers the boundary behavior of
// InsertEffectAt (negative -> 0, oversized -> len).
func TestInsertEffectAtClampsRange(t *testing.T) {
	s := ccg.NewState()
	s.PushEffect(ccg.Effect{Kind: "a"})
	s.PushEffect(ccg.Effect{Kind: "b"})
	negID := s.InsertEffectAt(-5, ccg.Effect{Kind: "front"})
	bigID := s.InsertEffectAt(100, ccg.Effect{Kind: "back"})
	if s.PendingEffects[0].ID != negID {
		t.Fatalf("negative idx should clamp to front, got %+v", s.PendingEffects)
	}
	if s.PendingEffects[len(s.PendingEffects)-1].ID != bigID {
		t.Fatalf("oversized idx should clamp to back, got %+v", s.PendingEffects)
	}
}

// TestEffectQueueJSONRoundTrip verifies PendingEffects survives a
// JSON encode/decode (the engine's snapshot path goes through JSON).
func TestEffectQueueJSONRoundTrip(t *testing.T) {
	s := ccg.NewState()
	s.PushEffect(ccg.Effect{
		Kind: "damage", Source: 7, Controller: "1", Target: 9,
		Targets: []ccg.EntityID{9, 11}, Data: map[string]any{"amount": 3},
		Tag: "delayed:upkeep",
	})
	s.PushEffect(ccg.Effect{Kind: "draw", Countered: true})

	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var s2 ccg.State
	if err := json.Unmarshal(raw, &s2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(s2.PendingEffects) != 2 {
		t.Fatalf("expected 2 pending after round-trip, got %d", len(s2.PendingEffects))
	}
	got := s2.PendingEffects[0]
	if got.Kind != "damage" || got.Source != 7 || got.Controller != "1" ||
		got.Target != 9 || len(got.Targets) != 2 || got.Tag != "delayed:upkeep" {
		t.Fatalf("first effect did not survive round-trip: %+v", got)
	}
	// Note: JSON-decoded numeric Data values come back as float64 — that
	// matches the existing Entity.AttrInt contract.
	if amt, _ := got.Data["amount"].(float64); amt != 3 {
		t.Fatalf("expected amount=3 (float64 from json), got %v", got.Data["amount"])
	}
	if !s2.PendingEffects[1].Countered {
		t.Fatalf("Countered flag did not round-trip")
	}
}
