package modulebridge

import (
	"testing"

	"github.com/tjcran/boardgame-go/modules/ccg"
)

func shopOp(t *testing.T, name string) Op {
	t.Helper()
	for _, op := range ShopRegistry().Ops("shop") {
		if op.Name == name {
			return op
		}
	}
	t.Fatalf("op %q not registered", name)
	return Op{}
}

func buildShopState(t *testing.T) (map[string]any, *ccg.State, []string) {
	t.Helper()
	st := ccg.NewState()
	st.NewZone("slots", false)
	st.NewZone("stock", true)
	st.NewZone("hand", false)
	toks := make([]string, 0, 3)
	for i := 0; i < 3; i++ {
		id := st.NewEntity("item", "", map[string]any{"cost": 1})
		if err := st.MoveTo(id, "stock"); err != nil {
			t.Fatalf("seed stock: %v", err)
		}
		toks = append(toks, EntityToken(id))
	}
	return map[string]any{"ccg": st, "shop": &emptyState{}}, st, toks
}

func TestShop_FillBuyFreeze(t *testing.T) {
	mods, st, _ := buildShopState(t)

	if _, err := shopOp(t, "fill").Call(mods, map[string]any{"slots": "slots", "stock": "stock", "size": int64(2)}); err != nil {
		t.Fatalf("fill: %v", err)
	}
	if st.Size("slots") != 2 || st.Size("stock") != 1 {
		t.Fatalf("after fill: slots=%d stock=%d, want 2/1", st.Size("slots"), st.Size("stock"))
	}

	first := EntityToken(st.Zones["slots"].Members[0])
	if _, err := shopOp(t, "buy").Call(mods, map[string]any{"slots": "slots", "item": first, "dest": "hand"}); err != nil {
		t.Fatalf("buy: %v", err)
	}
	if st.Size("hand") != 1 || st.Size("slots") != 1 {
		t.Fatalf("after buy: hand=%d slots=%d, want 1/1", st.Size("hand"), st.Size("slots"))
	}

	keep := EntityToken(st.Zones["slots"].Members[0])
	if _, err := shopOp(t, "freeze").Call(mods, map[string]any{"slots": "slots", "item": keep}); err != nil {
		t.Fatalf("freeze: %v", err)
	}
	frRes, _ := shopOp(t, "is_frozen").Call(mods, map[string]any{"item": keep})
	if frRes.(bool) != true {
		t.Fatalf("is_frozen = %v, want true", frRes)
	}
	if _, err := shopOp(t, "clear").Call(mods, map[string]any{"slots": "slots", "dest": "stock"}); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if st.Size("slots") != 1 {
		t.Fatalf("after clear with frozen: slots=%d, want 1", st.Size("slots"))
	}
}

func TestShop_BuyNotInSlots(t *testing.T) {
	mods, _, toks := buildShopState(t)
	if _, err := shopOp(t, "buy").Call(mods, map[string]any{"slots": "slots", "item": toks[0], "dest": "hand"}); err == nil {
		t.Fatal("expected ErrNotInSlots")
	}
}
