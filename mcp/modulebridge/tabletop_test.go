package modulebridge

import "testing"

func ttOp(t *testing.T, name string) Op {
	t.Helper()
	for _, op := range TabletopRegistry().Ops("tabletop") {
		if op.Name == name {
			return op
		}
	}
	t.Fatalf("op %q not registered", name)
	return Op{}
}

func TestTabletop_StateFactory(t *testing.T) {
	st := NewState("tabletop")
	if _, ok := st.(*tabletopState); !ok {
		t.Fatalf("NewState(tabletop) = %T, want *tabletopState", st)
	}
}

func TestTabletop_NewBoard_Square(t *testing.T) {
	st := NewState("tabletop")
	if _, err := ttOp(t, "new_board").Call(st, map[string]any{"kind": "square", "w": int64(8), "h": int64(8)}); err != nil {
		t.Fatalf("new_board: %v", err)
	}
	res, err := ttOp(t, "distance").Call(st, map[string]any{"ax": int64(0), "ay": int64(0), "bx": int64(3), "by": int64(2)})
	if err != nil {
		t.Fatalf("distance: %v", err)
	}
	if res.(int64) != 3 {
		t.Fatalf("square distance = %v, want 3", res)
	}
}

func TestTabletop_NewBoard_RejectsUnknownKind(t *testing.T) {
	st := NewState("tabletop")
	if _, err := ttOp(t, "new_board").Call(st, map[string]any{"kind": "triangular", "w": int64(4), "h": int64(4)}); err == nil {
		t.Fatal("expected error for unknown board kind")
	}
}

func TestTabletop_DistanceWithoutBoard_Errors(t *testing.T) {
	st := NewState("tabletop")
	if _, err := ttOp(t, "distance").Call(st, map[string]any{"ax": int64(0), "ay": int64(0), "bx": int64(1), "by": int64(1)}); err == nil {
		t.Fatal("expected error: distance before new_board")
	}
}
