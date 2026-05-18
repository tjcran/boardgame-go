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

func TestSquareBoardInBounds(t *testing.T) {
	b := tabletop.NewSquareBoard(10, 8)
	cases := []struct {
		p    tabletop.Pos
		want bool
	}{
		{tabletop.Pos{0, 0}, true},
		{tabletop.Pos{9, 7}, true},
		{tabletop.Pos{10, 0}, false},
		{tabletop.Pos{0, 8}, false},
		{tabletop.Pos{-1, 0}, false},
		{tabletop.Pos{0, -1}, false},
	}
	for _, c := range cases {
		if got := b.InBounds(c.p); got != c.want {
			t.Errorf("InBounds(%v) = %v, want %v", c.p, got, c.want)
		}
	}
}

func TestSquareBoardDistanceIsChebyshev(t *testing.T) {
	b := tabletop.NewSquareBoard(20, 20)
	// Chebyshev (king's move) distance: max(|dx|, |dy|). Diagonal moves
	// cost the same as orthogonal — standard for square-grid wargames.
	cases := []struct {
		a, b tabletop.Pos
		want int
	}{
		{tabletop.Pos{0, 0}, tabletop.Pos{3, 0}, 3},
		{tabletop.Pos{0, 0}, tabletop.Pos{0, 5}, 5},
		{tabletop.Pos{0, 0}, tabletop.Pos{3, 5}, 5}, // diagonal
		{tabletop.Pos{2, 7}, tabletop.Pos{2, 7}, 0},
		{tabletop.Pos{5, 5}, tabletop.Pos{2, 2}, 3},
	}
	for _, c := range cases {
		if got := b.Distance(c.a, c.b); got != c.want {
			t.Errorf("Distance(%v,%v) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestSquareBoardNeighborsEightWay(t *testing.T) {
	b := tabletop.NewSquareBoard(10, 10)
	got := b.Neighbors(tabletop.Pos{5, 5})
	if len(got) != 8 {
		t.Fatalf("expected 8 neighbors for interior cell, got %d: %v", len(got), got)
	}
	// Corner — only 3 in-bounds neighbors.
	corner := b.Neighbors(tabletop.Pos{0, 0})
	if len(corner) != 3 {
		t.Fatalf("expected 3 neighbors at corner, got %d: %v", len(corner), corner)
	}
}

func TestSquareBoardLineIncludesEndpoints(t *testing.T) {
	b := tabletop.NewSquareBoard(10, 10)
	line := b.Line(tabletop.Pos{0, 0}, tabletop.Pos{3, 0})
	want := []tabletop.Pos{{0, 0}, {1, 0}, {2, 0}, {3, 0}}
	if len(line) != len(want) {
		t.Fatalf("line length = %d, want %d: %v", len(line), len(want), line)
	}
	for i, p := range want {
		if line[i] != p {
			t.Errorf("line[%d] = %v, want %v", i, line[i], p)
		}
	}
}

func TestSquareBoardLineDiagonal(t *testing.T) {
	b := tabletop.NewSquareBoard(10, 10)
	line := b.Line(tabletop.Pos{0, 0}, tabletop.Pos{3, 3})
	// Bresenham on a pure diagonal: every cell is on the line.
	want := []tabletop.Pos{{0, 0}, {1, 1}, {2, 2}, {3, 3}}
	if len(line) != len(want) {
		t.Fatalf("diagonal line length = %d, want %d: %v", len(line), len(want), line)
	}
	for i, p := range want {
		if line[i] != p {
			t.Errorf("diagonal line[%d] = %v, want %v", i, line[i], p)
		}
	}
}

func TestSquareBoardLineReverseSwapsEndpoints(t *testing.T) {
	b := tabletop.NewSquareBoard(10, 10)
	// Line a→b and b→a should both start at the first arg and end at the
	// second arg. We don't require the interior cells to be identical
	// (Bresenham is not symmetric on near-diagonals) — but the endpoints
	// MUST be where the caller said.
	line := b.Line(tabletop.Pos{3, 0}, tabletop.Pos{0, 0})
	if line[0] != (tabletop.Pos{3, 0}) || line[len(line)-1] != (tabletop.Pos{0, 0}) {
		t.Fatalf("Line endpoints must match args: got %v", line)
	}
}

func TestLineOfSightOnSquareBoardWithCoverBetween(t *testing.T) {
	b := tabletop.NewSquareBoard(10, 10)
	cover := map[tabletop.Pos]bool{{X: 2, Y: 0}: true}
	clear := tabletop.LineOfSight(b, tabletop.Pos{0, 0}, tabletop.Pos{4, 0}, func(p tabletop.Pos) bool {
		return cover[p]
	})
	if clear {
		t.Fatalf("expected blocked LOS through cover cell (2,0)")
	}
}
