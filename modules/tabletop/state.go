package tabletop

import (
	"encoding/json"
	"sort"
)

// State tracks the spatial layer: which UnitID is at which Pos, plus
// an internal reverse index for fast cell→units queries. State is
// game-state — embed it (or hold a pointer) in your G alongside ccg
// state and engine fields.
//
// State also optionally holds the Board geometry and a TerrainMap so
// that the entire tabletop context can be serialised as a single value.
// MarshalJSON/UnmarshalJSON handle the Board interface (which default
// encoding/json cannot reconstruct) via MarshalBoard/UnmarshalBoard.
type State struct {
	// Board is the optional geometry for this state. May be nil if the
	// caller manages the board separately (e.g. in a wargameG wrapper).
	Board Board `json:"-"`
	// Terrain is the optional per-cell tag store. May be nil.
	Terrain *TerrainMap `json:"-"`
	// Positions is the canonical unit → cell map. Marshalled.
	Positions map[UnitID]Pos `json:"positions,omitempty"`
	// byCell is the reverse index, built lazily. Not serialised — it's
	// derivable from Positions.
	byCell map[Pos][]UnitID
}

// NewState returns an empty spatial State.
func NewState() *State {
	return &State{
		Positions: map[UnitID]Pos{},
		byCell:    map[Pos][]UnitID{},
	}
}

// stateJSON is the on-wire shape for a State value.
type stateJSON struct {
	Board     json.RawMessage    `json:"board,omitempty"`
	Terrain   *TerrainMap        `json:"terrain,omitempty"`
	Positions map[UnitID]Pos     `json:"positions,omitempty"`
}

// MarshalJSON encodes State to JSON, using MarshalBoard for the Board
// interface field so that the concrete type can be recovered on unmarshal.
func (s *State) MarshalJSON() ([]byte, error) {
	sj := stateJSON{
		Terrain:   s.Terrain,
		Positions: s.Positions,
	}
	if s.Board != nil {
		raw, err := MarshalBoard(s.Board)
		if err != nil {
			return nil, err
		}
		sj.Board = json.RawMessage(raw)
	}
	return json.Marshal(sj)
}

// UnmarshalJSON decodes State from JSON, reconstructing the Board concrete
// type via UnmarshalBoard. A missing or null board key is treated as nil.
func (s *State) UnmarshalJSON(data []byte) error {
	var sj stateJSON
	if err := json.Unmarshal(data, &sj); err != nil {
		return err
	}
	if len(sj.Board) > 0 && string(sj.Board) != "null" {
		b, err := UnmarshalBoard(sj.Board)
		if err != nil {
			return err
		}
		s.Board = b
	}
	s.Terrain = sj.Terrain
	s.Positions = sj.Positions
	// byCell is rebuilt lazily; leave it nil so ensureIndex triggers on
	// first EntitiesAt/Within call.
	s.byCell = nil
	return nil
}

// Place puts unit at pos. If unit already has a position, it's moved —
// equivalent to Move. Replay-safe: equal inputs → equal state.
func (s *State) Place(unit UnitID, pos Pos) {
	if s.Positions == nil {
		s.Positions = map[UnitID]Pos{}
	}
	if old, ok := s.Positions[unit]; ok {
		s.removeFromCell(old, unit)
	}
	s.Positions[unit] = pos
	s.addToCell(pos, unit)
}

// Move is an alias for Place — same semantics, kept for readability at
// call sites where "moving an existing unit" is the natural verb.
func (s *State) Move(unit UnitID, pos Pos) { s.Place(unit, pos) }

// Remove deletes the unit from the spatial state. No-op when unit has
// no recorded position.
func (s *State) Remove(unit UnitID) {
	pos, ok := s.Positions[unit]
	if !ok {
		return
	}
	s.removeFromCell(pos, unit)
	delete(s.Positions, unit)
}

// PositionOf returns the unit's current position, plus an existence
// bool. Use the bool to distinguish "at (0,0)" from "not placed."
func (s *State) PositionOf(unit UnitID) (Pos, bool) {
	pos, ok := s.Positions[unit]
	return pos, ok
}

// EntitiesAt returns the UnitIDs currently at pos, sorted ascending
// for deterministic iteration (replay safety). Returns nil for empty
// cells. Always reads through the reverse index; rebuilds lazily after
// a JSON unmarshal.
func (s *State) EntitiesAt(pos Pos) []UnitID {
	s.ensureIndex()
	src := s.byCell[pos]
	if len(src) == 0 {
		return nil
	}
	out := make([]UnitID, len(src))
	copy(out, src)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Within returns UnitIDs whose position is within `radius` cells of
// `center`, measured by board.Distance. Result is sorted by UnitID
// ascending. radius < 0 returns nil.
//
// Implementation: iterates the full Positions map, distance-filtering
// each. Fine up to ~a few thousand units; for larger battles a spatial
// hash on top of byCell would be the next step.
func (s *State) Within(board Board, center Pos, radius int) []UnitID {
	if radius < 0 {
		return nil
	}
	var out []UnitID
	for id, p := range s.Positions {
		if board.Distance(center, p) <= radius {
			out = append(out, id)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func (s *State) ensureIndex() {
	if s.byCell != nil {
		return
	}
	s.byCell = map[Pos][]UnitID{}
	for id, p := range s.Positions {
		s.byCell[p] = append(s.byCell[p], id)
	}
}

func (s *State) addToCell(pos Pos, unit UnitID) {
	if s.byCell == nil {
		// First touch — rebuild from Positions, which already includes
		// the newly-assigned (unit, pos) entry. No manual append needed.
		s.ensureIndex()
		return
	}
	s.byCell[pos] = append(s.byCell[pos], unit)
}

func (s *State) removeFromCell(pos Pos, unit UnitID) {
	s.ensureIndex()
	cell := s.byCell[pos]
	for i, u := range cell {
		if u == unit {
			s.byCell[pos] = append(cell[:i], cell[i+1:]...)
			if len(s.byCell[pos]) == 0 {
				delete(s.byCell, pos)
			}
			return
		}
	}
}
