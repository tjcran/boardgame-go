package tabletop

import (
	"encoding/json"
	"fmt"
)

// Board is the geometry primitive. Implementations are pure: they
// return positions and distances without consulting any game state.
// Layer game state (terrain, units, fog of war) on top via the free
// functions in this package (LineOfSight) or your own.
type Board interface {
	// InBounds reports whether p is a valid cell on this board.
	InBounds(p Pos) bool
	// Distance is the grid distance between a and b in cells. The
	// metric is impl-defined (Chebyshev for square, hex distance for
	// hex). Both arguments need not be in bounds; the result is still
	// the geometric distance.
	Distance(a, b Pos) int
	// Neighbors returns the in-bounds adjacent cells of p. Order is
	// impl-defined but deterministic.
	Neighbors(p Pos) []Pos
	// Line returns the cells along the straight line from a to b
	// inclusive. a is always first, b is always last. Out-of-bounds
	// cells are NOT filtered — callers wanting to ignore them must
	// check InBounds themselves.
	Line(a, b Pos) []Pos
}

// boardJSON is the on-wire representation used by MarshalBoard/UnmarshalBoard.
type boardJSON struct {
	Kind string `json:"kind"`
	W    int    `json:"w"`
	H    int    `json:"h"`
}

// MarshalBoard encodes a Board to JSON using a kind discriminator so the
// concrete type can be recovered by UnmarshalBoard.
func MarshalBoard(b Board) ([]byte, error) {
	switch t := b.(type) {
	case *SquareBoard:
		return json.Marshal(boardJSON{"square", t.Width, t.Height})
	case *HexBoard:
		return json.Marshal(boardJSON{"hex", t.Width, t.Height})
	default:
		return nil, fmt.Errorf("tabletop: unmarshalable board %T", b)
	}
}

// UnmarshalBoard reconstructs the concrete Board that was encoded by
// MarshalBoard.
func UnmarshalBoard(raw []byte) (Board, error) {
	var bj boardJSON
	if err := json.Unmarshal(raw, &bj); err != nil {
		return nil, err
	}
	switch bj.Kind {
	case "square":
		return NewSquareBoard(bj.W, bj.H), nil
	case "hex":
		return NewHexBoard(bj.W, bj.H), nil
	default:
		return nil, fmt.Errorf("tabletop: unknown board kind %q", bj.Kind)
	}
}

// LineOfSight reports whether sight is clear from `from` to `target` on
// board b. It walks b.Line(from, target) and asks `blocks` about every
// intermediate cell (endpoints excluded — you can always see and be
// seen from a cell you occupy). Returns true when no intermediate cell
// blocks.
//
// LineOfSight is board-agnostic — pass a SquareBoard or a HexBoard and
// it Just Works.
func LineOfSight(b Board, from, target Pos, blocks func(Pos) bool) bool {
	if blocks == nil {
		return true
	}
	line := b.Line(from, target)
	if len(line) <= 2 {
		// from and target are adjacent (or identical) — no intermediate cells.
		return true
	}
	for _, p := range line[1 : len(line)-1] {
		if blocks(p) {
			return false
		}
	}
	return true
}
