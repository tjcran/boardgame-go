package modulebridge

import (
	"testing"

	"github.com/tjcran/boardgame-go/modules/ccg"
	"github.com/tjcran/boardgame-go/modules/tabletop"
)

func fixtureModules(t *testing.T) map[string]any {
	t.Helper()
	mods := map[string]any{
		"tabletop": NewState("tabletop"),
		"ccg":      NewState("ccg"),
		"regions":  NewState("regions"),
	}
	if mods["regions"] == nil {
		t.Fatalf("regions state factory not registered")
	}
	return mods
}

func callOp(t *testing.T, mods map[string]any, mod, name string, args map[string]any) any {
	t.Helper()
	reg := RegistryFor(mod)
	if reg == nil {
		t.Fatalf("no registry for %q", mod)
	}
	for _, op := range reg.Ops(mod) {
		if op.Name == name {
			out, err := op.Call(mods, args, nil)
			if err != nil {
				t.Fatalf("%s.%s: %v", mod, name, err)
			}
			return out
		}
	}
	t.Fatalf("op %s.%s not found", mod, name)
	return nil
}

func TestRegionsNewMapAndOf(t *testing.T) {
	mods := fixtureModules(t)
	callOp(t, mods, "regions", "new_map", map[string]any{
		"regions": []any{
			map[string]any{
				"id":    "north",
				"cells": []any{[]any{0, 0}, []any{1, 0}},
			},
			map[string]any{
				"id":    "south",
				"cells": []any{[]any{0, 1}, []any{1, 1}},
			},
		},
	})
	out := callOp(t, mods, "regions", "of", map[string]any{
		"x": int64(0), "y": int64(0),
	}).(map[string]any)
	if out["region"] != "north" || out["found"] != true {
		t.Errorf("of = %v, want region:north found:true", out)
	}
	miss := callOp(t, mods, "regions", "of", map[string]any{
		"x": int64(9), "y": int64(9),
	}).(map[string]any)
	if miss["found"] != false {
		t.Errorf("of miss: found = %v, want false", miss["found"])
	}
}

func TestRegionsCells(t *testing.T) {
	mods := fixtureModules(t)
	callOp(t, mods, "regions", "new_map", map[string]any{
		"regions": []any{
			map[string]any{"id": "n", "cells": []any{[]any{0, 0}, []any{1, 0}}},
		},
	})
	out := callOp(t, mods, "regions", "cells", map[string]any{
		"id": "n",
	}).([]any)
	if len(out) != 2 {
		t.Fatalf("cells len = %d, want 2", len(out))
	}
}

func TestRegionsInfluenceAndScoreAll(t *testing.T) {
	mods := fixtureModules(t)
	callOp(t, mods, "regions", "new_map", map[string]any{
		"regions": []any{
			map[string]any{"id": "n", "cells": []any{[]any{0, 0}, []any{1, 0}}},
		},
	})
	cs := mods["ccg"].(*ccg.State)
	idA := cs.NewEntity("unit", "", map[string]any{"owner": "alice"})
	idB := cs.NewEntity("unit", "", map[string]any{"owner": "bob"})
	ts := mods["tabletop"].(*tabletopState)
	ts.Space.Place(tabletop.UnitID(idA), tabletop.Pos{X: 0, Y: 0})
	ts.Space.Place(tabletop.UnitID(idB), tabletop.Pos{X: 1, Y: 0})

	infl := callOp(t, mods, "regions", "influence", map[string]any{}).(map[string]any)
	per := infl["n"].(map[string]any)
	if per["alice"] == nil || per["bob"] == nil {
		t.Fatalf("influence missing players: %v", per)
	}

	out := callOp(t, mods, "regions", "score_all", map[string]any{
		"rule": map[string]any{
			"kind":     "plurality",
			"perPlace": []any{int64(3)},
			"tieBreak": "split",
		},
	}).(map[string]any)
	if out["alice"] == nil || out["bob"] == nil {
		t.Fatalf("score_all missing players: %v", out)
	}
}

func TestRegionsRehydrateRegistered(t *testing.T) {
	rh := rehydrators["regions"]
	if rh == nil {
		t.Fatalf("regions rehydrator not registered")
	}
}
