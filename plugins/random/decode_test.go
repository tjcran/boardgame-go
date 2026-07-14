package random

import (
	"encoding/json"
	"testing"

	"github.com/tjcran/boardgame-go/core"
)

// TestDecodeExactUint64 pins the reason Decode exists: PRNG states are
// full-range uint64, and a float64 round-trip (the map[string]any path)
// loses precision above 2^53. Decode must restore the exact value.
func TestDecodeExactUint64(t *testing.T) {
	p := New(nil)
	const want = uint64(18446744073709551615) // max uint64
	raw, _ := json.Marshal(state{S: want})
	got, err := p.Decode(raw)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	s, ok := got.(*state)
	if !ok {
		t.Fatalf("Decode returned %T, want *state", got)
	}
	if s.S != want {
		t.Fatalf("Decode S = %d, want %d", s.S, want)
	}
}

// TestAPIDegradesInsteadOfPanicking covers legacy loaders that skip the
// manager's PluginDecode pass: API must never panic on generic JSON data.
func TestAPIDegradesInsteadOfPanicking(t *testing.T) {
	p := New(uint64(7))
	r := p.API(map[string]any{"s": float64(123456)}, nil, core.Ctx{}, "", nil)
	if r == nil {
		t.Fatal("API returned nil for map data")
	}
	r = p.API(nil, nil, core.Ctx{}, "", nil)
	if r == nil {
		t.Fatal("API returned nil for nil data")
	}
}
