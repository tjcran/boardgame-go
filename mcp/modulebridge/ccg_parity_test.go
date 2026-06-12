package modulebridge

import (
	"testing"

	"github.com/tjcran/boardgame-go/modules/ccg"
)

func TestModifierAndEffectTokens(t *testing.T) {
	if tok := ModifierToken(ccg.ModifierID(7)); tok != "mod:7" {
		t.Fatalf("ModifierToken = %q, want mod:7", tok)
	}
	id, err := ParseModifierToken("mod:7")
	if err != nil || id != 7 {
		t.Fatalf("ParseModifierToken = (%v, %v), want (7, nil)", id, err)
	}
	if _, err := ParseModifierToken("ent:7"); err == nil {
		t.Fatal("ParseModifierToken must reject foreign prefixes")
	}
	if tok := EffectToken(ccg.EffectID(3)); tok != "eff:3" {
		t.Fatalf("EffectToken = %q, want eff:3", tok)
	}
	eid, err := ParseEffectToken("eff:3")
	if err != nil || eid != 3 {
		t.Fatalf("ParseEffectToken = (%v, %v), want (3, nil)", eid, err)
	}
	if _, err := ParseEffectToken("eff:"); err == nil {
		t.Fatal("ParseEffectToken must reject empty payload")
	}
}

func TestCCG_ModifierOps(t *testing.T) {
	st := ccg.NewState()
	st.NewZone("battlefield", true)
	aura := st.NewEntity("aura", "0", nil)
	if err := st.Add("battlefield", aura); err != nil {
		t.Fatal(err)
	}
	creature := st.NewEntity("creature", "0", map[string]any{"power": 2})
	mods := map[string]any{"ccg": st}

	res, err := ccgOp(t, "add_modifier").Call(mods, map[string]any{
		"target": EntityToken(creature), "attribute": "power", "op": "add", "value": 2,
		"source": EntityToken(aura), "expires_with": EntityToken(aura),
		"while_in": []any{"battlefield"},
	}, nil)
	if err != nil {
		t.Fatalf("add_modifier: %v", err)
	}
	modTok, _ := res.(string)
	if modTok != "mod:1" {
		t.Fatalf("add_modifier token = %q, want mod:1", modTok)
	}

	val, err := ccgOp(t, "effective_attr").Call(mods, map[string]any{
		"entity": EntityToken(creature), "attr": "power", "default": 0,
	}, nil)
	if err != nil {
		t.Fatalf("effective_attr: %v", err)
	}
	if v, _ := val.(int64); v != 4 {
		t.Fatalf("effective power = %v, want 4", val)
	}

	// Aura leaves the battlefield: sweep reports the expired modifier.
	st.NewZone("graveyard", true)
	if err := st.MoveTo(aura, "graveyard"); err != nil {
		t.Fatal(err)
	}
	swept, err := ccgOp(t, "sweep_modifiers").Call(mods, nil, nil)
	if err != nil {
		t.Fatalf("sweep_modifiers: %v", err)
	}
	sweptList, _ := swept.([]any)
	if len(sweptList) != 1 || sweptList[0] != "mod:1" {
		t.Fatalf("sweep_modifiers = %v, want [mod:1]", swept)
	}

	// remove_modifier on a fresh modifier.
	res2, err := ccgOp(t, "add_modifier").Call(mods, map[string]any{
		"target": EntityToken(creature), "attribute": "power", "op": "set", "value": 9,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ccgOp(t, "remove_modifier").Call(mods, map[string]any{"modifier": res2}, nil); err != nil {
		t.Fatalf("remove_modifier: %v", err)
	}
	if len(st.Modifiers) != 0 {
		t.Fatalf("modifier not removed, %d remain", len(st.Modifiers))
	}
}

func TestCCG_CounterOps(t *testing.T) {
	st := ccg.NewState()
	id := st.NewEntity("creature", "0", nil)
	mods := map[string]any{"ccg": st}

	if _, err := ccgOp(t, "add_counter").Call(mods, map[string]any{
		"entity": EntityToken(id), "kind": "+1/+1", "n": 3,
	}, nil); err != nil {
		t.Fatalf("add_counter: %v", err)
	}
	if _, err := ccgOp(t, "remove_counter").Call(mods, map[string]any{
		"entity": EntityToken(id), "kind": "+1/+1", "n": 1,
	}, nil); err != nil {
		t.Fatalf("remove_counter: %v", err)
	}
	res, err := ccgOp(t, "counters").Call(mods, map[string]any{
		"entity": EntityToken(id), "kind": "+1/+1",
	}, nil)
	if err != nil {
		t.Fatalf("counters: %v", err)
	}
	if n, _ := res.(int64); n != 2 {
		t.Fatalf("counters = %v, want 2", res)
	}
}

func TestCCG_EffectOps(t *testing.T) {
	st := ccg.NewState()
	src := st.NewEntity("spell", "0", nil)
	mods := map[string]any{"ccg": st}

	res, err := ccgOp(t, "push_effect").Call(mods, map[string]any{
		"kind": "damage", "source": EntityToken(src), "controller": "0",
		"data": map[string]any{"amount": 3},
	}, nil)
	if err != nil {
		t.Fatalf("push_effect: %v", err)
	}
	first, _ := res.(string)

	// front: true prepends (stack-top semantics).
	res2, err := ccgOp(t, "push_effect").Call(mods, map[string]any{
		"kind": "counterspell", "front": true,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	top, _ := res2.(string)

	pending, err := ccgOp(t, "pending_effects").Call(mods, nil, nil)
	if err != nil {
		t.Fatalf("pending_effects: %v", err)
	}
	list, _ := pending.([]any)
	if len(list) != 2 {
		t.Fatalf("pending_effects len = %d, want 2", len(list))
	}
	head, _ := list[0].(map[string]any)
	if head["id"] != top || head["kind"] != "counterspell" {
		t.Fatalf("queue head = %v, want the fronted counterspell", head)
	}
	if d, _ := list[1].(map[string]any); d["data"].(map[string]any)["amount"] != 3 {
		t.Fatalf("effect data lost: %v", list[1])
	}

	if _, err := ccgOp(t, "replace_effect").Call(mods, map[string]any{
		"effect": first, "kind": "damage", "data": map[string]any{"amount": 1},
	}, nil); err != nil {
		t.Fatalf("replace_effect: %v", err)
	}
	eff, _, ok := st.FindEffect(ccg.EffectID(1))
	if !ok || eff.Data["amount"] != 1 {
		t.Fatalf("replace_effect did not take: %+v ok=%v", eff, ok)
	}

	if _, err := ccgOp(t, "remove_effect").Call(mods, map[string]any{"effect": top}, nil); err != nil {
		t.Fatalf("remove_effect: %v", err)
	}
	if len(st.PendingEffects) != 1 {
		t.Fatalf("pending = %d after remove, want 1", len(st.PendingEffects))
	}

	// Unknown effect handles error rather than silently no-op.
	if _, err := ccgOp(t, "remove_effect").Call(mods, map[string]any{"effect": "eff:99"}, nil); err == nil {
		t.Fatal("remove_effect on unknown id must error")
	}
}

func TestCCG_CloneOp(t *testing.T) {
	st := ccg.NewState()
	orig := st.NewEntity("creature", "0", map[string]any{"power": 2})
	mods := map[string]any{"ccg": st}

	res, err := ccgOp(t, "clone").Call(mods, map[string]any{
		"entity": EntityToken(orig), "owner": "1",
		"overrides": map[string]any{"token": true},
	}, nil)
	if err != nil {
		t.Fatalf("clone: %v", err)
	}
	id, err := ParseEntityToken(res.(string))
	if err != nil {
		t.Fatal(err)
	}
	e, ok := st.Get(id)
	if !ok || e.Owner != "1" || !e.AttrBool("token", false) || e.AttrInt("power", 0) != 2 {
		t.Fatalf("clone result wrong: %+v", e)
	}
}

func TestCCG_ControllerAndVisibilityOps(t *testing.T) {
	st := ccg.NewState()
	id := st.NewEntity("creature", "0", map[string]any{"name": "dragon"})
	mods := map[string]any{"ccg": st}

	if _, err := ccgOp(t, "set_controller").Call(mods, map[string]any{
		"entity": EntityToken(id), "controller": "1",
	}, nil); err != nil {
		t.Fatalf("set_controller: %v", err)
	}
	if _, err := ccgOp(t, "set_visibility").Call(mods, map[string]any{
		"entity": EntityToken(id), "visibility": "owner_only",
	}, nil); err != nil {
		t.Fatalf("set_visibility: %v", err)
	}
	if _, err := ccgOp(t, "set_visibility").Call(mods, map[string]any{
		"entity": EntityToken(id), "visibility": "bogus",
	}, nil); err == nil {
		t.Fatal("set_visibility must reject unknown values")
	}
	if _, err := ccgOp(t, "reveal_to").Call(mods, map[string]any{
		"entity": EntityToken(id), "viewers": []any{"2", "3"},
	}, nil); err != nil {
		t.Fatalf("reveal_to: %v", err)
	}
	if _, err := ccgOp(t, "conceal_from").Call(mods, map[string]any{
		"entity": EntityToken(id), "viewer": "3",
	}, nil); err != nil {
		t.Fatalf("conceal_from: %v", err)
	}
	if _, err := ccgOp(t, "set_mask").Call(mods, map[string]any{
		"entity": EntityToken(id), "mask": map[string]any{"frame": "creature"},
	}, nil); err != nil {
		t.Fatalf("set_mask: %v", err)
	}

	e, _ := st.Get(id)
	if e.Controller != "1" || e.Visibility != ccg.OwnerOnly {
		t.Fatalf("controller/visibility wrong: %+v", e)
	}
	if len(e.RevealedTo) != 1 || e.RevealedTo[0] != "2" {
		t.Fatalf("RevealedTo = %v, want [2]", e.RevealedTo)
	}
	if e.Mask["frame"] != "creature" {
		t.Fatalf("Mask = %v", e.Mask)
	}
}

func TestCCG_QueryOp(t *testing.T) {
	st := ccg.NewState()
	st.NewZone("battlefield", true)
	a := st.NewEntity("creature", "0", map[string]any{"power": 4})
	b := st.NewEntity("creature", "1", map[string]any{"power": 2})
	c := st.NewEntity("land", "0", nil)
	for _, id := range []ccg.EntityID{a, b, c} {
		if err := st.Add("battlefield", id); err != nil {
			t.Fatal(err)
		}
	}
	st.SetController(b, "0")
	mods := map[string]any{"ccg": st}

	res, err := ccgOp(t, "query").Call(mods, map[string]any{
		"zone": "battlefield", "type": "creature", "controlled_by": "0",
	}, nil)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	ids, _ := res.([]any)
	if len(ids) != 2 || ids[0] != EntityToken(a) || ids[1] != EntityToken(b) {
		t.Fatalf("query = %v, want [%s %s]", ids, EntityToken(a), EntityToken(b))
	}

	res, err = ccgOp(t, "query").Call(mods, map[string]any{
		"owned_by": "0", "attr_at_least": map[string]any{"attr": "power", "min": 3},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ids, _ = res.([]any)
	if len(ids) != 1 || ids[0] != EntityToken(a) {
		t.Fatalf("query owned_by+attr_at_least = %v, want [%s]", ids, EntityToken(a))
	}
}

func TestCCG_PeekOp(t *testing.T) {
	st := ccg.NewState()
	st.NewZone("deck", true)
	a := st.NewEntity("card", "0", nil)
	b := st.NewEntity("card", "0", nil)
	if err := st.Add("deck", a); err != nil {
		t.Fatal(err)
	}
	if err := st.Add("deck", b); err != nil {
		t.Fatal(err)
	}
	res, err := ccgOp(t, "peek").Call(map[string]any{"ccg": st}, map[string]any{"zone": "deck", "n": 1}, nil)
	if err != nil {
		t.Fatalf("peek: %v", err)
	}
	toks, _ := res.([]any)
	if len(toks) != 1 {
		t.Fatalf("peek = %v, want one token", res)
	}
}

func TestCCG_ParityReadOnlyFlags(t *testing.T) {
	wantRO := map[string]bool{
		"effective_attr": true, "counters": true, "pending_effects": true,
		"query": true, "peek": true,
		"add_modifier": false, "remove_modifier": false, "sweep_modifiers": false,
		"add_counter": false, "remove_counter": false,
		"push_effect": false, "remove_effect": false, "replace_effect": false,
		"clone": false, "set_controller": false, "set_visibility": false,
		"reveal_to": false, "conceal_from": false, "set_mask": false,
	}
	for name, ro := range wantRO {
		if got := ccgOp(t, name).ReadOnly; got != ro {
			t.Errorf("op %s ReadOnly = %v, want %v", name, got, ro)
		}
	}
}
