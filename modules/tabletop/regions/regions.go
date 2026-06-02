package regions

import "github.com/tjcran/boardgame-go/modules/tabletop"

// RegionID is the stable, JSON-safe name of a region. Use semantic
// names ("anduin", "north_continent") over opaque IDs — designers,
// replays, and MCP traces all read this.
type RegionID string

// Region is one named cell group. Cells must be in-bounds for the
// associated Board and must not overlap with any other region in the
// same Map. The library does NOT enforce contiguity.
type Region struct {
	ID    RegionID       `json:"id"`
	Cells []tabletop.Pos `json:"cells"`
	Label string         `json:"label,omitempty"`
}

// Map is the partition of a board into named regions. Build once at
// Setup time (or on first move via mc.Random if cell assignment is
// random) and store on G. Map itself is JSON-safe; the lazy reverse
// index is rebuilt on first Of() call after unmarshal.
type Map struct {
	Regions []Region `json:"regions"`

	cellToRegion map[tabletop.Pos]RegionID
}

// OwnerFn maps a tabletop.UnitID to the playerID that owns it.
// Returning "" means "no owner" — the unit occupies its cell but
// contributes to no player's influence.
type OwnerFn func(tabletop.UnitID) string
