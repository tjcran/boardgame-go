package tabletop

// UnitID is the opaque handle for a positioned entity. tabletop never
// allocates IDs itself — callers pass them in. Games that also use
// ccg can cast directly: `tabletop.UnitID(ccgEntity.ID)`.
type UnitID uint64

// Pos is a 2D integer position. The Board impl interprets the
// coordinates: SquareBoard reads them as (col, row); HexBoard reads
// them as axial (q, r). Pos values are comparable and map-key safe.
type Pos struct {
	X, Y int
}
