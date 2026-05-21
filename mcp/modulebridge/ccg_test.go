package modulebridge

import (
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

	if _, err := ccgOp(t, "new_zone").Call(map[string]any{"ccg": st}, map[string]any{"name": "hand", "ordered": false}); err != nil {
		t.Fatalf("new_zone: %v", err)
	}
	res, err := ccgOp(t, "new_entity").Call(map[string]any{"ccg": st}, map[string]any{"type": "card", "owner": "0"})
	if err != nil {
		t.Fatalf("new_entity: %v", err)
	}
	tok, _ := res.(string)
	if tok != "ent:1" {
		t.Fatalf("got token %q, want ent:1", tok)
	}
	if _, err := ccgOp(t, "move_to").Call(map[string]any{"ccg": st}, map[string]any{"entity": tok, "zone": "hand"}); err != nil {
		t.Fatalf("move_to: %v", err)
	}
	sizeRes, err := ccgOp(t, "size").Call(map[string]any{"ccg": st}, map[string]any{"zone": "hand"})
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
	_, err := ccgOp(t, "move_to").Call(map[string]any{"ccg": st}, map[string]any{"entity": "ent:99", "zone": "hand"})
	if err == nil {
		t.Fatal("expected ErrUnknownEntity for missing entity")
	}
}

func TestCCG_StateFactory(t *testing.T) {
	if _, ok := NewState("ccg").(*ccg.State); !ok {
		t.Fatal("NewState(ccg) did not return *ccg.State")
	}
}
