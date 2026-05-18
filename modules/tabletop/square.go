package tabletop

// SquareBoard is a rectangular grid with Chebyshev (king's-move)
// distance — the standard for square-grid wargames where diagonal
// movement costs the same as orthogonal. Cells are indexed (0,0) at
// top-left to (Width-1, Height-1) at bottom-right. Width and Height
// must be > 0; the constructor clamps them.
type SquareBoard struct {
	Width, Height int
}

// NewSquareBoard returns a Board sized w × h. Sizes ≤ 0 clamp to 1 so
// the result is always usable.
func NewSquareBoard(w, h int) *SquareBoard {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	return &SquareBoard{Width: w, Height: h}
}

// InBounds reports whether p is on the board.
func (b *SquareBoard) InBounds(p Pos) bool {
	return p.X >= 0 && p.X < b.Width && p.Y >= 0 && p.Y < b.Height
}

// Distance is Chebyshev distance: max(|dx|, |dy|).
func (b *SquareBoard) Distance(a, c Pos) int {
	dx := a.X - c.X
	if dx < 0 {
		dx = -dx
	}
	dy := a.Y - c.Y
	if dy < 0 {
		dy = -dy
	}
	if dx > dy {
		return dx
	}
	return dy
}

// Neighbors returns the up to 8 in-bounds king-move neighbors of p.
// Order: N, NE, E, SE, S, SW, W, NW (clockwise from north) — stable
// for replay determinism.
func (b *SquareBoard) Neighbors(p Pos) []Pos {
	offsets := [8][2]int{
		{0, -1}, {1, -1}, {1, 0}, {1, 1},
		{0, 1}, {-1, 1}, {-1, 0}, {-1, -1},
	}
	out := make([]Pos, 0, 8)
	for _, o := range offsets {
		n := Pos{X: p.X + o[0], Y: p.Y + o[1]}
		if b.InBounds(n) {
			out = append(out, n)
		}
	}
	return out
}

// Line returns the Bresenham line from a to b inclusive. Endpoints are
// guaranteed to be line[0] and line[len-1] respectively, in the order
// the caller supplied them. Cells beyond the board are NOT filtered —
// callers that need that should check b.InBounds.
func (b *SquareBoard) Line(a, target Pos) []Pos {
	dx := target.X - a.X
	if dx < 0 {
		dx = -dx
	}
	dy := target.Y - a.Y
	if dy < 0 {
		dy = -dy
	}
	sx := 1
	if a.X >= target.X {
		sx = -1
		if a.X == target.X {
			sx = 0
		}
	}
	sy := 1
	if a.Y >= target.Y {
		sy = -1
		if a.Y == target.Y {
			sy = 0
		}
	}
	err := dx - dy
	x, y := a.X, a.Y
	out := []Pos{{X: x, Y: y}}
	for x != target.X || y != target.Y {
		e2 := 2 * err
		if e2 > -dy {
			err -= dy
			x += sx
		}
		if e2 < dx {
			err += dx
			y += sy
		}
		out = append(out, Pos{X: x, Y: y})
	}
	return out
}
