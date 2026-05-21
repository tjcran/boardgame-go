package modulebridge

import (
	"fmt"
	"testing"

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
