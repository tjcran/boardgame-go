package starlarkgame

import (
	"testing"

	"github.com/tjcran/boardgame-go/modules/ccg"
)

func TestValidateArgs_PrimitivesAndCount(t *testing.T) {
	mv := Move{Name: "m", ArgsDef: []ArgDef{{Name: "n", Type: "int"}, {Name: "s", Type: "string"}}}
	if err := validateArgs(mv, []any{int64(3), "x"}, nil); err != nil {
		t.Fatalf("valid: %v", err)
	}
	if err := validateArgs(mv, []any{int64(3)}, nil); err == nil {
		t.Fatal("expected arg-count mismatch error")
	}
	if err := validateArgs(mv, []any{"notint", "x"}, nil); err == nil {
		t.Fatal("expected int type error")
	}
}

func TestValidateArgs_NoDefIsUnconstrained(t *testing.T) {
	mv := Move{Name: "m"}
	if err := validateArgs(mv, []any{int64(1), "a", true}, nil); err != nil {
		t.Fatalf("argless move should be unconstrained: %v", err)
	}
}

func TestValidateArgs_IntMinMax(t *testing.T) {
	lo, hi := int64(1), int64(5)
	mv := Move{Name: "m", ArgsDef: []ArgDef{{Name: "n", Type: "int", Min: &lo, Max: &hi}}}
	if err := validateArgs(mv, []any{int64(3)}, nil); err != nil {
		t.Fatalf("in range: %v", err)
	}
	if err := validateArgs(mv, []any{int64(9)}, nil); err == nil {
		t.Fatal("expected out-of-range error")
	}
}

func TestValidateArgs_EntityZone(t *testing.T) {
	st := ccg.NewState()
	st.NewZone("hand", false)
	st.NewZone("deck", true)
	id := st.NewEntity("card", "0", nil)
	st.MoveTo(id, "deck")
	mods := map[string]any{"ccg": st}

	mv := Move{Name: "play", ArgsDef: []ArgDef{{Name: "c", Type: "entity", Zone: "hand"}}}
	if err := validateArgs(mv, []any{"ent:1"}, mods); err == nil {
		t.Fatal("expected reject: entity not in zone hand")
	}
	st.MoveTo(id, "hand")
	if err := validateArgs(mv, []any{"ent:1"}, mods); err != nil {
		t.Fatalf("entity in hand should pass: %v", err)
	}
	if err := validateArgs(mv, []any{"nope"}, mods); err == nil {
		t.Fatal("expected reject: bad entity token")
	}
}

func TestValidateArgs_Hex(t *testing.T) {
	mv := Move{Name: "move", ArgsDef: []ArgDef{{Name: "to", Type: "hex"}}}
	if err := validateArgs(mv, []any{[]any{int64(2), int64(3)}}, nil); err != nil {
		t.Fatalf("valid hex: %v", err)
	}
	if err := validateArgs(mv, []any{[]any{int64(2)}}, nil); err == nil {
		t.Fatal("expected reject: hex needs 2 ints")
	}
	if err := validateArgs(mv, []any{"2,3"}, nil); err == nil {
		t.Fatal("expected reject: hex must be a list")
	}
}

func TestRenderArgSig_EntityZone(t *testing.T) {
	if got := renderArgSig(ArgDef{Name: "card", Type: "entity", Zone: "hand"}); got != "card: entity(hand)" {
		t.Fatalf("got %q, want card: entity(hand)", got)
	}
	if got := renderArgSig(ArgDef{Name: "c", Type: "entity"}); got != "c: entity" {
		t.Fatalf("got %q, want c: entity", got)
	}
}
