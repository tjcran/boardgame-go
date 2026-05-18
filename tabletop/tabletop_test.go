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

// stubBoard is a 5-cell horizontal line: (0,0), (1,0), …, (4,0).
type stubBoard struct{}

func (stubBoard) InBounds(p tabletop.Pos) bool { return p.Y == 0 && p.X >= 0 && p.X <= 4 }
func (stubBoard) Distance(a, b tabletop.Pos) int {
	d := a.X - b.X
	if d < 0 {
		d = -d
	}
	return d
}
func (stubBoard) Neighbors(p tabletop.Pos) []tabletop.Pos {
	var out []tabletop.Pos
	for _, dx := range []int{-1, 1} {
		n := tabletop.Pos{X: p.X + dx, Y: p.Y}
		if (stubBoard{}).InBounds(n) {
			out = append(out, n)
		}
	}
	return out
}
func (stubBoard) Line(a, b tabletop.Pos) []tabletop.Pos {
	if a.X > b.X {
		a, b = b, a
	}
	out := make([]tabletop.Pos, 0, b.X-a.X+1)
	for x := a.X; x <= b.X; x++ {
		out = append(out, tabletop.Pos{X: x, Y: 0})
	}
	return out
}

func TestLineOfSightClearWithNoBlockers(t *testing.T) {
	b := stubBoard{}
	clear := tabletop.LineOfSight(b, tabletop.Pos{0, 0}, tabletop.Pos{4, 0}, func(p tabletop.Pos) bool {
		return false
	})
	if !clear {
		t.Fatalf("expected clear LOS with no blockers")
	}
}

func TestLineOfSightBlockedByIntermediateCell(t *testing.T) {
	b := stubBoard{}
	clear := tabletop.LineOfSight(b, tabletop.Pos{0, 0}, tabletop.Pos{4, 0}, func(p tabletop.Pos) bool {
		return p.X == 2
	})
	if clear {
		t.Fatalf("expected blocked LOS when an intermediate cell blocks")
	}
}

func TestLineOfSightIgnoresEndpoints(t *testing.T) {
	b := stubBoard{}
	// A blocker on the source or target itself does NOT block LOS — only
	// intermediate cells matter (matches the standard tabletop rule:
	// you can see and be seen from a cell you occupy).
	clear := tabletop.LineOfSight(b, tabletop.Pos{0, 0}, tabletop.Pos{4, 0}, func(p tabletop.Pos) bool {
		return p.X == 0 || p.X == 4
	})
	if !clear {
		t.Fatalf("endpoints must not block their own LOS")
	}
}
