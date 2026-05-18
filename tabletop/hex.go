package tabletop

import "math"

// HexBoard is a hex grid using axial coordinates (q, r) stored on Pos
// as (X, Y). Width × Height defines a rectangular extent: 0 ≤ q < W,
// 0 ≤ r < H. Constructor clamps non-positive sizes to 1.
//
// Distance is hex distance: (|dq| + |dr| + |dq+dr|) / 2. Neighbors
// are the six axial offsets. Line uses cube-coordinate linear
// interpolation with hex rounding so the result is a connected
// chain of cells from a to b inclusive whose length equals
// Distance(a, b) + 1.
type HexBoard struct {
	Width, Height int
}

// NewHexBoard returns a hex board with rectangular axial extent w × h.
// Sizes ≤ 0 clamp to 1.
func NewHexBoard(w, h int) *HexBoard {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	return &HexBoard{Width: w, Height: h}
}

// InBounds reports whether p is in the rectangular axial extent.
func (b *HexBoard) InBounds(p Pos) bool {
	return p.X >= 0 && p.X < b.Width && p.Y >= 0 && p.Y < b.Height
}

// Distance is hex distance in axial coords.
func (b *HexBoard) Distance(a, c Pos) int {
	dq := a.X - c.X
	dr := a.Y - c.Y
	if dq < 0 {
		dq = -dq
	}
	if dr < 0 {
		dr = -dr
	}
	ds := (a.X - c.X) + (a.Y - c.Y)
	if ds < 0 {
		ds = -ds
	}
	return (dq + dr + ds) / 2
}

// hexOffsets are the six axial-neighbor directions, ordered for stable
// iteration (replay determinism).
var hexOffsets = [6][2]int{
	{1, 0}, {1, -1}, {0, -1},
	{-1, 0}, {-1, 1}, {0, 1},
}

// Neighbors returns the up to 6 in-bounds axial neighbors of p.
func (b *HexBoard) Neighbors(p Pos) []Pos {
	out := make([]Pos, 0, 6)
	for _, o := range hexOffsets {
		n := Pos{X: p.X + o[0], Y: p.Y + o[1]}
		if b.InBounds(n) {
			out = append(out, n)
		}
	}
	return out
}

// Line returns the connected hex chain from a to b inclusive, length
// Distance(a, b) + 1. Uses cube-coord lerp + cube round.
func (b *HexBoard) Line(a, target Pos) []Pos {
	n := b.Distance(a, target)
	if n == 0 {
		return []Pos{a}
	}
	// Convert axial → cube: x = q, z = r, y = -x - z.
	ax, az := float64(a.X), float64(a.Y)
	ay := -ax - az
	tx, tz := float64(target.X), float64(target.Y)
	ty := -tx - tz
	out := make([]Pos, 0, n+1)
	for i := 0; i <= n; i++ {
		t := float64(i) / float64(n)
		cx := ax + (tx-ax)*t
		cy := ay + (ty-ay)*t
		cz := az + (tz-az)*t
		rx, _, rz := cubeRound(cx, cy, cz)
		out = append(out, Pos{X: rx, Y: rz})
	}
	// Pin endpoints exactly — float lerp drift can otherwise shift the
	// last cell by one when very long lines round adversely.
	out[0] = a
	out[len(out)-1] = target
	return out
}

// cubeRound rounds (x, y, z) cube coords to the nearest valid cube hex
// (constraint: x + y + z == 0). Standard algorithm: round each, then
// snap the coord with the largest delta to satisfy the sum.
func cubeRound(x, y, z float64) (int, int, int) {
	rx := int(math.Round(x))
	ry := int(math.Round(y))
	rz := int(math.Round(z))
	dx := math.Abs(float64(rx) - x)
	dy := math.Abs(float64(ry) - y)
	dz := math.Abs(float64(rz) - z)
	if dx > dy && dx > dz {
		rx = -ry - rz
	} else if dy > dz {
		ry = -rx - rz
	} else {
		rz = -rx - ry
	}
	return rx, ry, rz
}
