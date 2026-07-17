package ccg

import (
	"errors"

	"github.com/tjcran/boardgame-go/core"
)

// ZoneName is the unique key for a zone (hand, deck, discard,
// battlefield, exile, …). Game authors pick their own; the library
// has no reserved names.
type ZoneName string

// Zone is an ordered or unordered collection of EntityIDs. Ordered
// zones preserve insertion order (decks); unordered zones can still
// iterate, but moves don't preserve position.
//
// Capacity caps len(Members). 0 means unlimited (default). Add,
// InsertAt, and cross-zone MoveTo return ErrZoneFull when the cap is
// reached. The library never auto-evicts: lowering Capacity below the
// current size leaves existing members in place but blocks new inserts
// until the zone shrinks below the cap.
type Zone struct {
	Name     ZoneName   `json:"name"`
	Ordered  bool       `json:"ordered,omitempty"`
	Capacity int        `json:"capacity,omitempty"`
	Members  []EntityID `json:"members,omitempty"`
}

// ErrUnknownZone / ErrUnknownEntity / ErrZoneEmpty / ErrInvalidPosition /
// ErrZoneFull are returned for the obvious failure modes.
var (
	ErrUnknownZone     = errors.New("ccg: unknown zone")
	ErrUnknownEntity   = errors.New("ccg: unknown entity")
	ErrZoneEmpty       = errors.New("ccg: zone is empty")
	ErrInvalidPosition = errors.New("ccg: invalid zone position")
	ErrZoneFull        = errors.New("ccg: zone at capacity")
)

// NewZone declares a zone. Idempotent — calling with an existing name
// is a no-op.
func (s *State) NewZone(name ZoneName, ordered bool) *Zone {
	if z, ok := s.Zones[name]; ok {
		return z
	}
	z := &Zone{Name: name, Ordered: ordered}
	if s.Zones == nil {
		s.Zones = map[ZoneName]*Zone{}
	}
	s.Zones[name] = z
	return z
}

// Add appends an entity to a zone. Updates Entity.Zone to keep the
// per-entity index consistent. Returns ErrZoneFull when the zone has
// a non-zero Capacity and is already at it; the rejected insert is
// atomic — Members and Entity.Zone are not touched.
func (s *State) Add(zone ZoneName, id EntityID) error {
	z, ok := s.Zones[zone]
	if !ok {
		return ErrUnknownZone
	}
	e, ok := s.Entities[id]
	if !ok {
		return ErrUnknownEntity
	}
	if z.Capacity > 0 && len(z.Members) >= z.Capacity {
		return ErrZoneFull
	}
	z.Members = append(z.Members, id)
	e.Zone = zone
	s.Entities[id] = e
	return nil
}

// InsertAt places an entity at a specific index in the zone. Position 0
// inserts at the front of Members (bottom of deck, since Draw pops from
// the end); position == len(Members) appends (top of deck), equivalent
// to Add. Out-of-range positions return ErrInvalidPosition. Updates
// Entity.Zone like Add. Does not auto-remove the entity from any prior
// zone — callers that need a move-with-position should Remove first.
func (s *State) InsertAt(zone ZoneName, id EntityID, position int) error {
	z, ok := s.Zones[zone]
	if !ok {
		return ErrUnknownZone
	}
	e, ok := s.Entities[id]
	if !ok {
		return ErrUnknownEntity
	}
	if position < 0 || position > len(z.Members) {
		return ErrInvalidPosition
	}
	if z.Capacity > 0 && len(z.Members) >= z.Capacity {
		return ErrZoneFull
	}
	z.Members = append(z.Members, 0)
	copy(z.Members[position+1:], z.Members[position:])
	z.Members[position] = id
	e.Zone = zone
	s.Entities[id] = e
	return nil
}

// Remove pops an entity from a zone. Returns ErrUnknownEntity if the
// entity isn't in this zone. Doesn't touch the entity itself — use
// Destroy to remove from the State entirely.
func (s *State) Remove(zone ZoneName, id EntityID) error {
	z, ok := s.Zones[zone]
	if !ok {
		return ErrUnknownZone
	}
	for i, m := range z.Members {
		if m == id {
			z.Members = append(z.Members[:i], z.Members[i+1:]...)
			return nil
		}
	}
	return ErrUnknownEntity
}

// MoveTo relocates an entity to a target zone. Bypasses the zone's
// current position lookup by reading Entity.Zone — O(1) instead of
// scanning every zone. If the entity has no Zone set, it's treated as
// new (appended to target only).
//
// Returns ErrZoneFull when the destination has a non-zero Capacity and
// the move would grow it past the cap. The check happens before the
// source-zone removal, so a rejected move leaves the entity in its
// original zone unchanged. Same-zone moves (e.Zone == to) skip the
// capacity check — they don't change member count.
func (s *State) MoveTo(id EntityID, to ZoneName) error {
	toZone, ok := s.Zones[to]
	if !ok {
		return ErrUnknownZone
	}
	e, ok := s.Entities[id]
	if !ok {
		return ErrUnknownEntity
	}
	if toZone.Capacity > 0 && e.Zone != to && len(toZone.Members) >= toZone.Capacity {
		return ErrZoneFull
	}
	if e.Zone != "" {
		if from, ok := s.Zones[e.Zone]; ok {
			for i, m := range from.Members {
				if m == id {
					from.Members = append(from.Members[:i], from.Members[i+1:]...)
					break
				}
			}
		}
	}
	toZone.Members = append(toZone.Members, id)
	e.Zone = to
	s.Entities[id] = e
	return nil
}

// Draw pops the top n entities from a zone and returns them. Top is
// the end of the Members slice for ordered zones — game authors who
// want "top of deck" should Add to the deck in cast/scry order and
// Draw the last element first; reverse if needed. n > len(zone)
// returns ErrZoneEmpty after taking whatever was available.
func (s *State) Draw(zone ZoneName, n int) ([]EntityID, error) {
	z, ok := s.Zones[zone]
	if !ok {
		return nil, ErrUnknownZone
	}
	if len(z.Members) == 0 {
		return nil, ErrZoneEmpty
	}
	if n > len(z.Members) {
		n = len(z.Members)
	}
	drawn := make([]EntityID, n)
	// Pop from the end (top) for ordered zones; same for unordered.
	for i := 0; i < n; i++ {
		idx := len(z.Members) - 1
		drawn[i] = z.Members[idx]
		z.Members = z.Members[:idx]
	}
	// Update entity zone — caller usually MoveTo's the drawn entities
	// into another zone but we conservatively clear theirs.
	for _, id := range drawn {
		if e, ok := s.Entities[id]; ok {
			e.Zone = ""
			s.Entities[id] = e
		}
	}
	return drawn, nil
}

// Peek returns the top n entity IDs without modifying the zone.
// Useful for scry / look-at-the-top effects.
func (s *State) Peek(zone ZoneName, n int) []EntityID {
	z, ok := s.Zones[zone]
	if !ok {
		return nil
	}
	if n > len(z.Members) {
		n = len(z.Members)
	}
	out := make([]EntityID, n)
	for i := 0; i < n; i++ {
		out[i] = z.Members[len(z.Members)-1-i]
	}
	return out
}

// Shuffle randomises the zone in place using the engine's seeded PRNG.
// Pass mc.Random from a move; replay determinism depends on it.
func (s *State) Shuffle(zone ZoneName, r *core.Random) error {
	z, ok := s.Zones[zone]
	if !ok {
		return ErrUnknownZone
	}
	z.Members = core.Shuffle(r, z.Members)
	return nil
}

// Contains reports whether the zone holds the given entity.
func (s *State) Contains(zone ZoneName, id EntityID) bool {
	z, ok := s.Zones[zone]
	if !ok {
		return false
	}
	for _, m := range z.Members {
		if m == id {
			return true
		}
	}
	return false
}

// Size returns the entity count in the zone.
func (s *State) Size(zone ZoneName) int {
	z, ok := s.Zones[zone]
	if !ok {
		return 0
	}
	return len(z.Members)
}
