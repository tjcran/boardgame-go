package modulebridge

import (
	"testing"

	"github.com/tjcran/boardgame-go/modules/ccg"
)

func ecoOp(t *testing.T, name string) Op {
	t.Helper()
	for _, op := range EconomyRegistry().Ops("economy") {
		if op.Name == name {
			return op
		}
	}
	t.Fatalf("op %q not registered", name)
	return Op{}
}

func TestEconomy_GainSpendCurrent(t *testing.T) {
	st := ccg.NewState()
	owner := st.NewEntity("player", "0", nil)
	mods := map[string]any{"ccg": st, "economy": &emptyState{}}
	tok := EntityToken(owner)

	if _, err := ecoOp(t, "gain").Call(mods, map[string]any{"owner": tok, "kind": "gold", "cap": int64(10), "n": int64(6)}, nil); err != nil {
		t.Fatalf("gain: %v", err)
	}
	cur, err := ecoOp(t, "current").Call(mods, map[string]any{"owner": tok, "kind": "gold"}, nil)
	if err != nil {
		t.Fatalf("current: %v", err)
	}
	if cur.(int64) != 6 {
		t.Fatalf("current = %v, want 6", cur)
	}
	if _, err := ecoOp(t, "spend").Call(mods, map[string]any{"owner": tok, "kind": "gold", "n": int64(4)}, nil); err != nil {
		t.Fatalf("spend: %v", err)
	}
	cur2, _ := ecoOp(t, "current").Call(mods, map[string]any{"owner": tok, "kind": "gold"}, nil)
	if cur2.(int64) != 2 {
		t.Fatalf("after spend = %v, want 2", cur2)
	}
}

func TestEconomy_SpendInsufficient(t *testing.T) {
	st := ccg.NewState()
	owner := st.NewEntity("player", "0", nil)
	mods := map[string]any{"ccg": st, "economy": &emptyState{}}
	_, err := ecoOp(t, "spend").Call(mods, map[string]any{"owner": EntityToken(owner), "kind": "gold", "n": int64(3)}, nil)
	if err == nil {
		t.Fatal("expected insufficient-funds error")
	}
}

func TestEconomy_Scaled(t *testing.T) {
	mods := map[string]any{"ccg": ccg.NewState(), "economy": &emptyState{}}
	res, err := ecoOp(t, "scaled").Call(mods, map[string]any{"turn": int64(3), "base": int64(4), "per": int64(1), "max": int64(10)}, nil)
	if err != nil {
		t.Fatalf("scaled: %v", err)
	}
	if res.(int64) != 6 {
		t.Fatalf("scaled = %v, want 6", res)
	}
}
