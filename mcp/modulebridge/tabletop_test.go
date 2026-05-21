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

func TestTabletop_PlaceMovePositionEntities(t *testing.T) {
	st := NewState("tabletop")
	ttOp(t, "new_board").Call(st, map[string]any{"kind": "square", "w": int64(8), "h": int64(8)})

	if _, err := ttOp(t, "place").Call(st, map[string]any{"unit": int64(1), "x": int64(2), "y": int64(3)}); err != nil {
		t.Fatalf("place: %v", err)
	}
	posRes, err := ttOp(t, "position_of").Call(st, map[string]any{"unit": int64(1)})
	if err != nil {
		t.Fatalf("position_of: %v", err)
	}
	xy := posRes.([]any)
	if xy[0].(int64) != 2 || xy[1].(int64) != 3 {
		t.Fatalf("position_of = %v, want [2 3]", xy)
	}
	none, err := ttOp(t, "position_of").Call(st, map[string]any{"unit": int64(99)})
	if err != nil {
		t.Fatalf("position_of(99): %v", err)
	}
	if none != nil {
		t.Fatalf("position_of(99) = %v, want nil", none)
	}

	ttOp(t, "move").Call(st, map[string]any{"unit": int64(1), "x": int64(4), "y": int64(4)})
	atRes, err := ttOp(t, "entities_at").Call(st, map[string]any{"x": int64(4), "y": int64(4)})
	if err != nil {
		t.Fatalf("entities_at: %v", err)
	}
	at := atRes.([]any)
	if len(at) != 1 || at[0].(int64) != 1 {
		t.Fatalf("entities_at(4,4) = %v, want [1]", at)
	}

	if _, err := ttOp(t, "remove").Call(st, map[string]any{"unit": int64(1)}); err != nil {
		t.Fatalf("remove: %v", err)
	}
	atRes2, _ := ttOp(t, "entities_at").Call(st, map[string]any{"x": int64(4), "y": int64(4)})
	if len(atRes2.([]any)) != 0 {
		t.Fatalf("entities_at after remove = %v, want []", atRes2)
	}
}

func TestTabletop_NeighborsWithinLOSTerrain(t *testing.T) {
	st := NewState("tabletop")
	ttOp(t, "new_board").Call(st, map[string]any{"kind": "square", "w": int64(8), "h": int64(8)})

	nRes, err := ttOp(t, "neighbors").Call(st, map[string]any{"x": int64(4), "y": int64(4)})
	if err != nil {
		t.Fatalf("neighbors: %v", err)
	}
	if len(nRes.([]any)) != 8 {
		t.Fatalf("neighbors count = %d, want 8", len(nRes.([]any)))
	}

	ttOp(t, "place").Call(st, map[string]any{"unit": int64(1), "x": int64(5), "y": int64(4)})
	ttOp(t, "place").Call(st, map[string]any{"unit": int64(2), "x": int64(0), "y": int64(0)})
	wRes, err := ttOp(t, "within").Call(st, map[string]any{"x": int64(4), "y": int64(4), "radius": int64(1)})
	if err != nil {
		t.Fatalf("within: %v", err)
	}
	w := wRes.([]any)
	if len(w) != 1 || w[0].(int64) != 1 {
		t.Fatalf("within(4,4,1) = %v, want [1]", w)
	}

	losRes, _ := ttOp(t, "line_of_sight").Call(st, map[string]any{"fx": int64(4), "fy": int64(2), "tx": int64(4), "ty": int64(4)})
	if losRes.(bool) != true {
		t.Fatalf("LOS before terrain = %v, want true", losRes)
	}
	if _, err := ttOp(t, "tag_terrain").Call(st, map[string]any{"x": int64(4), "y": int64(3), "tag": "blocks_los"}); err != nil {
		t.Fatalf("tag_terrain: %v", err)
	}
	hasRes, _ := ttOp(t, "has_terrain").Call(st, map[string]any{"x": int64(4), "y": int64(3), "tag": "blocks_los"})
	if hasRes.(bool) != true {
		t.Fatalf("has_terrain = %v, want true", hasRes)
	}
	losRes2, _ := ttOp(t, "line_of_sight").Call(st, map[string]any{"fx": int64(4), "fy": int64(2), "tx": int64(4), "ty": int64(4)})
	if losRes2.(bool) != false {
		t.Fatalf("LOS through blocker = %v, want false", losRes2)
	}
}
