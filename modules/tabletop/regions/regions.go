package regions

import (
	"errors"
	"fmt"

	"github.com/tjcran/boardgame-go/modules/tabletop"
)

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

// Sentinel errors returned by NewMap.
var (
	ErrEmptyID     = errors.New("regions: region has empty ID")
	ErrDuplicateID = errors.New("regions: duplicate region ID")
	ErrOverlap     = errors.New("regions: cell appears in more than one region")
)

// NewMap validates the partition (no empty IDs, no duplicate IDs, no
// overlapping cells) and returns a ready Map. Region order is preserved
// for stable replay digests. The lazy reverse index is built on first
// Of() call.
func NewMap(regions []Region) (*Map, error) {
	seen := make(map[RegionID]struct{}, len(regions))
	cells := make(map[tabletop.Pos]RegionID)
	out := make([]Region, 0, len(regions))
	for _, r := range regions {
		if r.ID == "" {
			return nil, ErrEmptyID
		}
		if _, ok := seen[r.ID]; ok {
			return nil, ErrDuplicateID
		}
		seen[r.ID] = struct{}{}
		for _, p := range r.Cells {
			if other, ok := cells[p]; ok {
				return nil, fmt.Errorf("%w: %v in %q and %q", ErrOverlap, p, other, r.ID)
			}
			cells[p] = r.ID
		}
		copied := append([]tabletop.Pos(nil), r.Cells...)
		out = append(out, Region{ID: r.ID, Cells: copied, Label: r.Label})
	}
	return &Map{Regions: out, cellToRegion: cells}, nil
}
