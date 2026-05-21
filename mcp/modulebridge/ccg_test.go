package modulebridge

import (
	"fmt"
	"testing"

	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/modules/ccg"
)

func ccgOp(t *testing.T, name string) Op {
	t.Helper()
	for _, op := range CCGRegistry().Ops("ccg") {
		if op.Name == name {
			return op
		}
	}
	t.Fatalf("op %q not registered", name)
	return Op{}
}

func TestCCG_NewZoneNewEntityMoveTo(t *testing.T) {
	st := ccg.NewState()

	if _, err := ccgOp(t, "new_zone").Call(map[string]any{"ccg": st}, map[string]any{"name": "hand", "ordered": false}, nil); err != nil {
		t.Fatalf("new_zone: %v", err)
	}
	res, err := ccgOp(t, "new_entity").Call(map[string]any{"ccg": st}, map[string]any{"type": "card", "owner": "0"}, nil)
	if err != nil {
		t.Fatalf("new_entity: %v", err)
	}
	tok, _ := res.(string)
	if tok != "ent:1" {
		t.Fatalf("got token %q, want ent:1", tok)
	}
	if _, err := ccgOp(t, "move_to").Call(map[string]any{"ccg": st}, map[string]any{"entity": tok, "zone": "hand"}, nil); err != nil {
		t.Fatalf("move_to: %v", err)
	}
	sizeRes, err := ccgOp(t, "size").Call(map[string]any{"ccg": st}, map[string]any{"zone": "hand"}, nil)
	if err != nil {
		t.Fatalf("size: %v", err)
	}
	if sizeRes.(int64) != 1 {
		t.Fatalf("got size %v, want 1", sizeRes)
	}
}

func TestCCG_MoveTo_BadHandle(t *testing.T) {
	st := ccg.NewState()
	st.NewZone("hand", false)
	_, err := ccgOp(t, "move_to").Call(map[string]any{"ccg": st}, map[string]any{"entity": "ent:99", "zone": "hand"}, nil)
	if err == nil {
		t.Fatal("expected ErrUnknownEntity for missing entity")
	}
}

func TestCCG_StateFactory(t *testing.T) {
	if _, ok := NewState("ccg").(*ccg.State); !ok {
		t.Fatal("NewState(ccg) did not return *ccg.State")
	}
}

func TestCCG_Publish_FiresSubscriber(t *testing.T) {
	st := ccg.NewState()
	fired := 0
	st.Subscribe(ccg.MatchType("died"), func(_ *ccg.State, e ccg.Event) {
		fired++
	})
	mods := map[string]any{"ccg": st}
	if _, err := ccgOp(t, "publish").Call(mods, map[string]any{"type": "died"}, nil); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if fired != 1 {
		t.Fatalf("subscriber fired %d times, want 1", fired)
	}
}

func TestCCG_Publish_RecoversHookError(t *testing.T) {
	st := ccg.NewState()
	st.Subscribe(ccg.MatchType("boom"), func(_ *ccg.State, e ccg.Event) {
		panic(HookError{Err: errTestHook})
	})
	mods := map[string]any{"ccg": st}
	_, err := ccgOp(t, "publish").Call(mods, map[string]any{"type": "boom"}, nil)
	if err == nil {
		t.Fatal("expected publish to surface the HookError panic as an error")
	}
}

var errTestHook = fmt.Errorf("boom hook failed")

func TestCCG_Shuffle_Deterministic(t *testing.T) {
	build := func() *ccg.State {
		s := ccg.NewState()
		s.NewZone("deck", true)
		for i := 0; i < 8; i++ {
			id := s.NewEntity("card", "", nil)
			_ = s.MoveTo(id, "deck")
		}
		return s
	}
	order := func(s *ccg.State) []ccg.EntityID {
		return append([]ccg.EntityID(nil), s.Zones["deck"].Members...)
	}
	a, b := build(), build()
	if _, err := ccgOp(t, "shuffle").Call(map[string]any{"ccg": a}, map[string]any{"zone": "deck"}, core.NewRandomFromState(seedPtr(42))); err != nil {
		t.Fatalf("shuffle a: %v", err)
	}
	if _, err := ccgOp(t, "shuffle").Call(map[string]any{"ccg": b}, map[string]any{"zone": "deck"}, core.NewRandomFromState(seedPtr(42))); err != nil {
		t.Fatalf("shuffle b: %v", err)
	}
	if !equalIDs(order(a), order(b)) {
		t.Fatalf("same seed gave different orders:\n a=%v\n b=%v", order(a), order(b))
	}
	c := build()
	ccgOp(t, "shuffle").Call(map[string]any{"ccg": c}, map[string]any{"zone": "deck"}, core.NewRandomFromState(seedPtr(7)))
	if len(order(c)) != 8 {
		t.Fatalf("shuffle changed deck size: %v", order(c))
	}
}

func TestCCG_Shuffle_UnknownZone(t *testing.T) {
	s := ccg.NewState()
	if _, err := ccgOp(t, "shuffle").Call(map[string]any{"ccg": s}, map[string]any{"zone": "nope"}, core.NewRandomFromState(seedPtr(1))); err == nil {
		t.Fatal("expected ErrUnknownZone")
	}
}

func TestCCG_Shuffle_NilRNG(t *testing.T) {
	s := ccg.NewState()
	s.NewZone("deck", true)
	if _, err := ccgOp(t, "shuffle").Call(map[string]any{"ccg": s}, map[string]any{"zone": "deck"}, nil); err == nil {
		t.Fatal("expected error when rng is nil")
	}
}

func seedPtr(v uint64) *uint64 { return &v }

func equalIDs(a, b []ccg.EntityID) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
