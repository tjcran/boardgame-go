package ccg_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/tjcran/boardgame-go/modules/ccg"
)

func TestStageTriggerMintsNoIDAndPublishesNothing(t *testing.T) {
	s := ccg.NewState()
	eventsBefore := len(s.Events)
	s.StageTrigger(ccg.Effect{Kind: "draw", ID: 42}) // caller-set ID is discarded
	if len(s.StagedTriggers) != 1 {
		t.Fatalf("staged buffer len = %d, want 1", len(s.StagedTriggers))
	}
	if s.StagedTriggers[0].ID != 0 {
		t.Errorf("staged effect ID = %d, want 0 (IDs mint at flush)", s.StagedTriggers[0].ID)
	}
	if len(s.Events) != eventsBefore {
		t.Errorf("StageTrigger must publish no events")
	}
	if len(s.PendingEffects) != 0 {
		t.Errorf("StageTrigger must not touch PendingEffects")
	}
}

func TestEffectFromStampsControllerFromEntity(t *testing.T) {
	s := ccg.NewState()
	owned := s.NewEntity("creature", "1", nil)
	eff := ccg.EffectFrom(s, owned, "draw", map[string]any{"n": 1})
	if eff.Controller != "1" {
		t.Errorf("Controller = %q, want owner %q", eff.Controller, "1")
	}
	if eff.Source != owned || eff.Kind != "draw" {
		t.Errorf("Source/Kind not stamped: %+v", eff)
	}

	s.SetController(owned, "0")
	if got := ccg.EffectFrom(s, owned, "draw", nil).Controller; got != "0" {
		t.Errorf("Controller should follow EffectiveController, got %q want %q", got, "0")
	}

	if got := ccg.EffectFrom(s, ccg.EntityID(999), "draw", nil).Controller; got != "" {
		t.Errorf("unknown source should leave Controller empty, got %q", got)
	}
}

func TestFlushTriggersMintsIDsInPushOrder(t *testing.T) {
	s := ccg.NewState()
	// Interleave a direct push so flush IDs continue the same sequence.
	first := s.PushEffect(ccg.Effect{Kind: "damage"})
	s.StageTrigger(ccg.Effect{Kind: "a"})
	s.StageTrigger(ccg.Effect{Kind: "b"})
	ids := s.FlushTriggers(nil)
	if len(ids) != 2 || ids[0] != first+1 || ids[1] != first+2 {
		t.Fatalf("flush IDs = %v, want [%d %d] continuing the shared counter", ids, first+1, first+2)
	}
	if len(s.StagedTriggers) != 0 {
		t.Errorf("buffer should be empty after flush")
	}
	kinds := []string{}
	for _, e := range s.PendingEffects {
		kinds = append(kinds, e.Kind)
	}
	if !reflect.DeepEqual(kinds, []string{"damage", "a", "b"}) {
		t.Errorf("PendingEffects order = %v, want [damage a b]", kinds)
	}
}

func TestFlushTriggersPublishesPushedPerEffect(t *testing.T) {
	s := ccg.NewState()
	var pushed int
	s.Subscribe(ccg.MatchType(ccg.EventEffectPushed), func(*ccg.State, ccg.Event) { pushed++ })
	s.StageTrigger(ccg.Effect{Kind: "a"})
	s.StageTrigger(ccg.Effect{Kind: "b"})
	s.FlushTriggers(nil)
	if pushed != 2 {
		t.Errorf("EventEffectPushed fired %d times, want 2", pushed)
	}
}

func TestFlushTriggersEmptyBufferIsNil(t *testing.T) {
	s := ccg.NewState()
	eventsBefore := len(s.Events)
	if ids := s.FlushTriggers(nil); ids != nil {
		t.Errorf("flush of empty buffer = %v, want nil", ids)
	}
	if len(s.Events) != eventsBefore {
		t.Errorf("empty flush must publish nothing")
	}
}

func TestFlushTriggersAppliesReorder(t *testing.T) {
	s := ccg.NewState()
	s.StageTrigger(ccg.Effect{Kind: "x", Controller: "0"})
	s.StageTrigger(ccg.Effect{Kind: "y", Controller: "1"})
	s.StageTrigger(ccg.Effect{Kind: "z", Controller: "0"})
	s.FlushTriggers(func(effs []ccg.Effect) []ccg.Effect {
		return ccg.OrderByPlayer(effs, []string{"1", "0"})
	})
	kinds := []string{}
	for _, e := range s.PendingEffects {
		kinds = append(kinds, e.Kind)
	}
	if !reflect.DeepEqual(kinds, []string{"y", "x", "z"}) {
		t.Errorf("reordered flush = %v, want [y x z]", kinds)
	}
}

// Handlers that fire on EventEffectPushed and stage more triggers must
// land in a NEW batch, not extend the one being flushed.
func TestFlushTriggersNestedStagingWaitsForNextCheckpoint(t *testing.T) {
	s := ccg.NewState()
	s.Subscribe(ccg.MatchType(ccg.EventEffectPushed), func(state *ccg.State, ev ccg.Event) {
		if n := len(state.PendingEffects); n > 0 && state.PendingEffects[n-1].Kind == "a" {
			state.StageTrigger(ccg.Effect{Kind: "nested"})
		}
	})
	s.StageTrigger(ccg.Effect{Kind: "a"})
	ids := s.FlushTriggers(nil)
	if len(ids) != 1 {
		t.Fatalf("first flush pushed %d effects, want 1 (nested waits)", len(ids))
	}
	if len(s.StagedTriggers) != 1 || s.StagedTriggers[0].Kind != "nested" {
		t.Fatalf("nested trigger should sit in the fresh buffer, got %+v", s.StagedTriggers)
	}
	ids = s.FlushTriggers(nil)
	if len(ids) != 1 || s.PendingEffects[len(s.PendingEffects)-1].Kind != "nested" {
		t.Errorf("second flush should push the nested trigger")
	}
}

// Staging from inside ResolveNext's EventEffectResolving publish must
// not disturb the resolution in flight.
func TestStageTriggerInsideResolvingHandler(t *testing.T) {
	s := ccg.NewState()
	src := s.NewEntity("permanent", "0", nil)
	s.Subscribe(ccg.MatchType(ccg.EventEffectResolving), func(state *ccg.State, ev ccg.Event) {
		state.StageTrigger(ccg.EffectFrom(state, src, "reactive", nil))
	})
	s.PushEffect(ccg.Effect{Kind: "damage"})
	resolved := false
	table := ccg.ResolverTable{"damage": func(*ccg.State, ccg.Effect) error { resolved = true; return nil }}
	eff, ok, err := s.ResolveNext(ccg.PickFront, table)
	if err != nil || !ok || !resolved || eff.Kind != "damage" {
		t.Fatalf("ResolveNext disturbed by staging handler: eff=%+v ok=%v err=%v", eff, ok, err)
	}
	if len(s.PendingEffects) != 0 {
		t.Errorf("resolved effect should be off the queue, got %d pending", len(s.PendingEffects))
	}
	if len(s.StagedTriggers) != 1 || s.StagedTriggers[0].Kind != "reactive" {
		t.Errorf("staged trigger missing: %+v", s.StagedTriggers)
	}
}

func TestOrderByPlayerStablePartition(t *testing.T) {
	effs := []ccg.Effect{
		{Kind: "a1", Controller: "0"},
		{Kind: "b1", Controller: "1"},
		{Kind: "a2", Controller: "0"},
		{Kind: "u1", Controller: "spectator"},
		{Kind: "b2", Controller: "1"},
		{Kind: "u2"},
	}
	got := ccg.OrderByPlayer(effs, []string{"1", "0"})
	kinds := []string{}
	for _, e := range got {
		kinds = append(kinds, e.Kind)
	}
	// Player 1's run first (staging order kept), then player 0's, then
	// unknowns in staging order.
	want := []string{"b1", "b2", "a1", "a2", "u1", "u2"}
	if !reflect.DeepEqual(kinds, want) {
		t.Errorf("OrderByPlayer = %v, want %v", kinds, want)
	}
	// Input untouched.
	if effs[0].Kind != "a1" || effs[5].Kind != "u2" {
		t.Errorf("OrderByPlayer must not mutate its input")
	}
}

func TestOrderByPlayerEmptyOrderKeepsStagingOrder(t *testing.T) {
	effs := []ccg.Effect{{Kind: "a"}, {Kind: "b", Controller: "0"}}
	got := ccg.OrderByPlayer(effs, nil)
	if !reflect.DeepEqual(got, effs) {
		t.Errorf("nil playerOrder should preserve staging order, got %v", got)
	}
}

func TestStagedTriggersSurviveJSONRoundTrip(t *testing.T) {
	s := ccg.NewState()
	src := s.NewEntity("permanent", "0", nil)
	s.StageTrigger(ccg.EffectFrom(s, src, "draw", map[string]any{"n": 1}))
	s.StageTrigger(ccg.Effect{Kind: "gain_life", Controller: "1"})

	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	restored := &ccg.State{}
	if err := json.Unmarshal(raw, restored); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(restored.StagedTriggers) != 2 {
		t.Fatalf("restored buffer len = %d, want 2", len(restored.StagedTriggers))
	}
	if restored.StagedTriggers[0].Kind != "draw" || restored.StagedTriggers[0].Controller != "0" {
		t.Errorf("restored[0] = %+v", restored.StagedTriggers[0])
	}

	// The flush after restore must mint IDs continuing the persisted
	// counter — same as if the process had never bounced.
	ids := restored.FlushTriggers(nil)
	if len(ids) != 2 || ids[0] != ccg.EffectID(1) || ids[1] != ccg.EffectID(2) {
		t.Errorf("post-restore flush IDs = %v, want [1 2]", ids)
	}
}

// Two runs of the same stage/flush sequence must produce identical
// queues — the replay-determinism property the staging buffer exists
// to protect.
func TestFlushTriggersDeterministicAcrossRuns(t *testing.T) {
	run := func() []ccg.Effect {
		s := ccg.NewState()
		e1 := s.NewEntity("permanent", "0", nil)
		e2 := s.NewEntity("permanent", "1", nil)
		// One event's worth of simultaneous triggers, two controllers.
		s.StageTrigger(ccg.EffectFrom(s, e1, "t1", nil))
		s.StageTrigger(ccg.EffectFrom(s, e2, "t2", nil))
		s.FlushTriggers(func(effs []ccg.Effect) []ccg.Effect {
			return ccg.OrderByPlayer(effs, []string{"0", "1"})
		})
		return s.PendingEffects
	}
	a, b := run(), run()
	if !reflect.DeepEqual(a, b) {
		t.Errorf("two identical runs diverged:\n%v\n%v", a, b)
	}
}
