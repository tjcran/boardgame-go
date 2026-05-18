package tabletop_test

import (
	"testing"

	"github.com/tjcran/boardgame-go/tabletop"
)

func TestPosIsComparable(t *testing.T) {
	a := tabletop.Pos{X: 1, Y: 2}
	b := tabletop.Pos{X: 1, Y: 2}
	c := tabletop.Pos{X: 2, Y: 1}
	if a != b {
		t.Fatalf("equal positions should compare equal")
	}
	if a == c {
		t.Fatalf("different positions should not compare equal")
	}
	// Map key safety — would not compile if Pos contained a slice/map.
	m := map[tabletop.Pos]int{a: 1}
	if m[b] != 1 {
		t.Fatalf("Pos should be usable as map key")
	}
}
