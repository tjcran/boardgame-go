package regions

import (
	"errors"
	"fmt"

	"github.com/tjcran/boardgame-go/modules/ccg"
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

// Of returns the region containing pos. The boolean is false when
// pos belongs to no region.
func (m *Map) Of(pos tabletop.Pos) (RegionID, bool) {
	if m.cellToRegion == nil {
		m.buildIndex()
	}
	id, ok := m.cellToRegion[pos]
	return id, ok
}

// Cells returns the cells of the named region, or nil if no such
// region exists. The returned slice is a defensive copy.
func (m *Map) Cells(id RegionID) []tabletop.Pos {
	for _, r := range m.Regions {
		if r.ID == id {
			return append([]tabletop.Pos(nil), r.Cells...)
		}
	}
	return nil
}

// buildIndex populates cellToRegion. Called lazily by Of after a
// JSON unmarshal nils the map.
func (m *Map) buildIndex() {
	m.cellToRegion = make(map[tabletop.Pos]RegionID)
	for _, r := range m.Regions {
		for _, p := range r.Cells {
			m.cellToRegion[p] = r.ID
		}
	}
}

// Influence walks every region's cells, asks the tabletop.State who is
// sitting in each cell, attributes each unit to its owner via owner,
// and tallies. The result is per-region, per-player count. Players who
// hold 0 influence in a region have no entry — callers should treat
// absence as zero.
//
// The outer map always contains an entry for every region in the Map
// (possibly empty), so callers can iterate Map.Regions or the result
// interchangeably.
func (m *Map) Influence(s *tabletop.State, owner OwnerFn) map[RegionID]map[string]int {
	out := make(map[RegionID]map[string]int, len(m.Regions))
	for _, r := range m.Regions {
		inner := map[string]int{}
		for _, p := range r.Cells {
			for _, u := range s.EntitiesAt(p) {
				pid := owner(u)
				if pid == "" {
					continue
				}
				inner[pid]++
			}
		}
		out[r.ID] = inner
	}
	return out
}

// ByCCGOwner is an OwnerFn that reads each unit's owner attribute from
// a ccg.State. The unitID is reinterpreted as a ccg.EntityID — they
// share uint64 representation, and the canonical ccg+tabletop pairing
// places a ccg entity onto the board as a tabletop unit. Unknown
// entities and entities with empty owner attribute return "".
func ByCCGOwner(cs *ccg.State) OwnerFn {
	return func(u tabletop.UnitID) string {
		e, ok := cs.Entities[ccg.EntityID(u)]
		if !ok {
			return ""
		}
		return e.AttrStr("owner", "")
	}
}
