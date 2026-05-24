package ccg

import "encoding/json"

// stateAlias is a defined type over State (not a `= State` alias) so it
// does NOT inherit State's MarshalJSON / UnmarshalJSON methods, breaking
// the recursion that would otherwise occur inside those methods.
type stateAlias State

// stateCountersJSON carries the unexported monotonic ID counters that
// are not visible to the default JSON encoder.
type stateCountersJSON struct {
	NextEntityID  uint64 `json:"nextEntityID"`
	NextModID     uint64 `json:"nextModID"`
	NextInsertion int    `json:"nextInsertion"`
	NextSubID     uint64 `json:"nextSubID"`
	NextAbilityID uint64 `json:"nextAbilityID"`
	NextEffectID  uint64 `json:"nextEffectID"`
}

// stateWire is the on-the-wire JSON shape: all exported fields (via
// stateAlias) plus a nested counters object under a reserved key.
// The counters key is additive — existing persisted matches that lack
// __counters__ simply unmarshal with zero counters (old behaviour),
// and fresh marshals will include the key going forward.
type stateWire struct {
	stateAlias
	Counters stateCountersJSON `json:"__counters__"`
}

// MarshalJSON encodes the State's exported fields plus the unexported
// monotonic ID counters. The exported-field wire shape is unchanged
// from the default encoding, so already-persisted matches remain
// decodable; __counters__ is purely additive.
func (s *State) MarshalJSON() ([]byte, error) {
	w := stateWire{
		stateAlias: stateAlias(*s),
		Counters: stateCountersJSON{
			NextEntityID:  s.nextEntityID,
			NextModID:     s.nextModID,
			NextInsertion: s.nextInsertion,
			NextSubID:     s.nextSubID,
			NextAbilityID: s.nextAbilityID,
			NextEffectID:  s.nextEffectID,
		},
	}
	return json.Marshal(w)
}

// UnmarshalJSON decodes the exported fields and, if present, restores
// the unexported monotonic ID counters from __counters__. Persisted
// data that pre-dates counter serialisation (missing __counters__) is
// decoded normally with counters at zero — a safe degraded mode.
// subs and abilities are intentionally NOT touched; they are
// re-registered per process by the game's Setup routine.
func (s *State) UnmarshalJSON(data []byte) error {
	var w stateWire
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	// Copy exported fields back into s (subs/abilities stay nil/zero).
	*s = State(w.stateAlias)
	// Restore the unexported counters.
	s.nextEntityID = w.Counters.NextEntityID
	s.nextModID = w.Counters.NextModID
	s.nextInsertion = w.Counters.NextInsertion
	s.nextSubID = w.Counters.NextSubID
	s.nextAbilityID = w.Counters.NextAbilityID
	s.nextEffectID = w.Counters.NextEffectID
	// Restore NewState's non-nil-map invariant: omitempty writes JSON null
	// for empty maps, so after a round-trip through an empty state the maps
	// land as nil. NewEntity / zone / modifier ops all assign into these
	// maps and will panic on nil — re-initialise any that were not populated
	// by the decode.
	if s.Entities == nil {
		s.Entities = map[EntityID]Entity{}
	}
	if s.Zones == nil {
		s.Zones = map[ZoneName]*Zone{}
	}
	if s.Modifiers == nil {
		s.Modifiers = map[ModifierID]Modifier{}
	}
	return nil
}
