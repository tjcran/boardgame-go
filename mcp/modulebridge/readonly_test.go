package modulebridge

import "testing"

func opReadOnly(t *testing.T, module, name string) bool {
	t.Helper()
	reg := RegistryFor(module)
	if reg == nil {
		t.Fatalf("no registry for module %q", module)
	}
	for _, op := range reg.Ops(module) {
		if op.Name == name {
			return op.ReadOnly
		}
	}
	t.Fatalf("op %s.%s not found", module, name)
	return false
}

func TestOpReadOnlyClassification(t *testing.T) {
	readOnly := map[string][]string{
		"ccg":      {"size", "members"},
		"tabletop": {"distance", "position_of", "entities_at", "within", "neighbors", "line_of_sight", "has_terrain"},
		"economy":  {"current", "scaled"},
		"shop":     {"is_frozen"},
	}
	mutating := map[string][]string{
		"ccg":      {"new_zone", "new_entity", "move_to", "draw", "publish", "shuffle"},
		"tabletop": {"new_board", "place", "move", "remove", "tag_terrain"},
		"economy":  {"gain", "spend", "set"},
		"shop":     {"clear", "fill", "roll", "buy", "freeze", "unfreeze"},
	}
	for mod, names := range readOnly {
		for _, n := range names {
			if !opReadOnly(t, mod, n) {
				t.Errorf("%s.%s: ReadOnly=false, want true", mod, n)
			}
		}
	}
	for mod, names := range mutating {
		for _, n := range names {
			if opReadOnly(t, mod, n) {
				t.Errorf("%s.%s: ReadOnly=true, want false", mod, n)
			}
		}
	}
}
