package starlarkgame

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/tjcran/boardgame-go/mcp/modulebridge"
)

// TestAsStarlarkG_RehydratesModules verifies that when a StarlarkG with live
// module states is marshaled and then decoded via asStarlarkG (simulating a
// serializing-store reload), each module entry is reconstructed as the same
// live typed state that modulebridge.NewState mints — not a plain map.
func TestAsStarlarkG_RehydratesModules(t *testing.T) {
	// A StarlarkG carrying a live tabletop module state.
	live := &StarlarkG{
		Data:    map[string]any{"x": int64(1)},
		Modules: map[string]any{"tabletop": modulebridge.NewState("tabletop")},
	}
	raw, err := json.Marshal(live)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Simulate a serializing-store reload: decode into a bare any (map).
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	sg, ok := asStarlarkG(decoded)
	if !ok {
		t.Fatalf("asStarlarkG failed")
	}

	mod := sg.Modules["tabletop"]
	if _, isMap := mod.(map[string]any); isMap {
		t.Fatalf("module not rehydrated: still a map[string]any")
	}
	if mod == nil {
		t.Fatalf("module missing after rehydrate")
	}
	// It must be the same concrete type modulebridge.NewState mints.
	want := modulebridge.NewState("tabletop")
	if got := reflect.TypeOf(mod); got != reflect.TypeOf(want) {
		t.Fatalf("rehydrated module type = %v, want %v", got, reflect.TypeOf(want))
	}
}

// TestAsStarlarkG_StarlarkGPointerUnchanged confirms that when a live
// *StarlarkG is passed to asStarlarkG (in-memory store path), the same
// pointer is returned with live module states untouched.
func TestAsStarlarkG_StarlarkGPointerUnchanged(t *testing.T) {
	modState := modulebridge.NewState("tabletop")
	sg := &StarlarkG{
		Data:    map[string]any{"y": int64(2)},
		Modules: map[string]any{"tabletop": modState},
	}

	got, ok := asStarlarkG(sg)
	if !ok {
		t.Fatalf("asStarlarkG failed for *StarlarkG input")
	}
	if got != sg {
		t.Fatalf("asStarlarkG returned a different pointer; want same *StarlarkG")
	}
	// Module entry must be the exact same value (live, not a map).
	if got.Modules["tabletop"] != modState {
		t.Fatalf("module state was replaced; want same live value")
	}
	if _, isMap := got.Modules["tabletop"].(map[string]any); isMap {
		t.Fatalf("module state became a map[string]any; want live typed state")
	}
}

// TestAsStarlarkG_ModuleFreeGame confirms that a module-free map (no
// __modules__ key) round-trips cleanly: Data is populated and Modules is empty.
func TestAsStarlarkG_ModuleFreeGame(t *testing.T) {
	input := map[string]any{
		"score": float64(42),
		"turn":  float64(3),
	}

	sg, ok := asStarlarkG(input)
	if !ok {
		t.Fatalf("asStarlarkG failed for module-free map")
	}
	if len(sg.Modules) != 0 {
		t.Fatalf("expected empty Modules, got %v", sg.Modules)
	}
	if sg.Data["score"] != float64(42) {
		t.Fatalf("Data[\"score\"] = %v, want 42", sg.Data["score"])
	}
	if sg.Data["turn"] != float64(3) {
		t.Fatalf("Data[\"turn\"] = %v, want 3", sg.Data["turn"])
	}
}
