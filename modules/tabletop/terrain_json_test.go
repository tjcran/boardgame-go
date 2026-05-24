package tabletop

import (
	"bytes"
	"encoding/json"
	"testing"
)

// TestTerrainMapJSON_Deterministic verifies that MarshalJSON produces
// byte-identical output for the same map contents across multiple calls, and
// that tags and cells survive a round-trip through JSON.
//
// Because Go map iteration is random, a non-deterministic MarshalJSON
// produces differently-ordered JSON each time. Rather than relying on
// repeated calls to catch a flaky non-determinism, the test also decodes the
// output and verifies that cells appear in (X then Y) order and that each
// cell's tags appear in alphabetical order — the invariants the sorted
// implementation guarantees.
func TestTerrainMapJSON_Deterministic(t *testing.T) {
	tm := NewTerrainMap()
	// Add several cells with multiple tags in non-alphabetical order.
	tm.Tag(Pos{X: 3, Y: 0}, "wall")
	tm.Tag(Pos{X: 3, Y: 0}, "blocks_los")
	tm.Tag(Pos{X: 1, Y: 2}, "difficult")
	tm.Tag(Pos{X: 1, Y: 2}, "cover")
	tm.Tag(Pos{X: 0, Y: 5}, "objective_zone")
	tm.Tag(Pos{X: 2, Y: 1}, "difficult")

	// Marshal twice — must produce identical bytes.
	b1, err := json.Marshal(tm)
	if err != nil {
		t.Fatalf("marshal 1: %v", err)
	}
	b2, err := json.Marshal(tm)
	if err != nil {
		t.Fatalf("marshal 2: %v", err)
	}
	if !bytes.Equal(b1, b2) {
		t.Fatalf("MarshalJSON is non-deterministic:\n  pass1: %s\n  pass2: %s", b1, b2)
	}

	// Decode and verify cell order is (X then Y) ascending.
	var cells []terrainCellJSON
	if err := json.Unmarshal(b1, &cells); err != nil {
		t.Fatalf("unmarshal decoded array: %v", err)
	}
	for i := 1; i < len(cells); i++ {
		prev, cur := cells[i-1].Pos, cells[i].Pos
		if cur.X < prev.X || (cur.X == prev.X && cur.Y <= prev.Y) {
			t.Fatalf("cells not in (X,Y) order at index %d: %v then %v", i, prev, cur)
		}
	}

	// Verify each cell's tags are in alphabetical order.
	for _, c := range cells {
		for j := 1; j < len(c.Tags); j++ {
			if c.Tags[j] < c.Tags[j-1] {
				t.Fatalf("tags not sorted at pos %v: %v", c.Pos, c.Tags)
			}
		}
	}

	// Round-trip: all original tags must survive.
	got := NewTerrainMap()
	if err := json.Unmarshal(b1, got); err != nil {
		t.Fatalf("unmarshal into TerrainMap: %v", err)
	}
	checks := []struct {
		p   Pos
		tag string
	}{
		{Pos{X: 3, Y: 0}, "wall"},
		{Pos{X: 3, Y: 0}, "blocks_los"},
		{Pos{X: 1, Y: 2}, "difficult"},
		{Pos{X: 1, Y: 2}, "cover"},
		{Pos{X: 0, Y: 5}, "objective_zone"},
		{Pos{X: 2, Y: 1}, "difficult"},
	}
	for _, ch := range checks {
		if !got.HasTag(ch.p, ch.tag) {
			t.Errorf("after round-trip: pos %v missing tag %q", ch.p, ch.tag)
		}
	}
}
