package ccg

import "github.com/tjcran/boardgame-go/core"

// Transition moves a batch of entity ids to the target zone with
// pre-flight all-or-nothing semantics: validates that every id exists
// and that `to` has enough remaining capacity for the inbound count
// (ids already in `to` don't grow the count) before mutating anything.
// On error returns one of ErrUnknownZone / ErrUnknownEntity /
// ErrZoneFull and leaves state untouched.
//
// Use for "move every targeted creature to the graveyard at once" /
// "move a player's whole hand to the discard pile" semantics where
// partial completion would corrupt downstream logic.
func (s *State) Transition(ids []EntityID, to ZoneName) error {
	toZone, ok := s.Zones[to]
	if !ok {
		return ErrUnknownZone
	}
	inbound := 0
	for _, id := range ids {
		e, ok := s.Entities[id]
		if !ok {
			return ErrUnknownEntity
		}
		if e.Zone != to {
			inbound++
		}
	}
	if toZone.Capacity > 0 && len(toZone.Members)+inbound > toZone.Capacity {
		return ErrZoneFull
	}
	for _, id := range ids {
		if err := s.MoveTo(id, to); err != nil {
			return err
		}
	}
	return nil
}

// MoveAllTo sweeps every entity from the given source zones into `to`,
// preserving source-zone order (then per-source-zone Members order).
// Same pre-flight all-or-nothing semantics as Transition. A source
// zone equal to `to` is silently skipped (no self-move).
func (s *State) MoveAllTo(to ZoneName, from ...ZoneName) error {
	toZone, ok := s.Zones[to]
	if !ok {
		return ErrUnknownZone
	}
	snapshots := make([][]EntityID, len(from))
	inbound := 0
	for i, fz := range from {
		z, ok := s.Zones[fz]
		if !ok {
			return ErrUnknownZone
		}
		snapshots[i] = make([]EntityID, len(z.Members))
		copy(snapshots[i], z.Members)
		if fz != to {
			inbound += len(z.Members)
		}
	}
	if toZone.Capacity > 0 && len(toZone.Members)+inbound > toZone.Capacity {
		return ErrZoneFull
	}
	for i, fz := range from {
		if fz == to {
			continue
		}
		for _, id := range snapshots[i] {
			if err := s.MoveTo(id, to); err != nil {
				return err
			}
		}
	}
	return nil
}

// Top returns the top entity of a zone — the end of Members, the side
// Draw pops from. Returns (0, false) for unknown or empty zones.
func (s *State) Top(zone ZoneName) (EntityID, bool) {
	z, ok := s.Zones[zone]
	if !ok || len(z.Members) == 0 {
		return 0, false
	}
	return z.Members[len(z.Members)-1], true
}

// Bottom returns the bottom entity of a zone — the start of Members,
// the side InsertAt(0) targets. Returns (0, false) for unknown or
// empty zones.
func (s *State) Bottom(zone ZoneName) (EntityID, bool) {
	z, ok := s.Zones[zone]
	if !ok || len(z.Members) == 0 {
		return 0, false
	}
	return z.Members[0], true
}

// Mulligan sweeps every entity from `hand` into `deck`, shuffles
// `deck` using the engine PRNG, then draws `n` back into `hand`.
// Deterministic given a deterministic *core.Random. n ≤ 0 short-
// circuits after the shuffle (useful for "redraw zero" / setup
// reshuffles).
//
// Returns the redrawn entity ids in draw order (top of deck first).
func (s *State) Mulligan(hand, deck ZoneName, n int, r *core.Random) ([]EntityID, error) {
	if err := s.MoveAllTo(deck, hand); err != nil {
		return nil, err
	}
	if err := s.Shuffle(deck, r); err != nil {
		return nil, err
	}
	if n <= 0 {
		return nil, nil
	}
	drawn, err := s.Draw(deck, n)
	if err != nil {
		return nil, err
	}
	if err := s.Transition(drawn, hand); err != nil {
		return drawn, err
	}
	return drawn, nil
}

// Deal instantiates one entity per DefID in `defs` for each player and
// adds it to `zoneFor(player)`. Iterates players outer, defs inner;
// returns the entity ids in (player, def) order for replay-stable
// reasoning. Useful for opening-hand setup where every seat starts
// with the same template list.
//
// Caller is expected to shuffle or randomise separately — Deal itself
// is deterministic. Returns ErrUnknownDef / ErrUnknownZone /
// ErrZoneFull on the first failure; already-dealt entities stay in
// place.
func (s *State) Deal(c *Catalog, defs []DefID, players []string, zoneFor func(player string) ZoneName) ([]EntityID, error) {
	out := make([]EntityID, 0, len(defs)*len(players))
	for _, p := range players {
		zone := zoneFor(p)
		for _, defID := range defs {
			id, err := s.Instantiate(c, defID, p)
			if err != nil {
				return out, err
			}
			if err := s.Add(zone, id); err != nil {
				return out, err
			}
			out = append(out, id)
		}
	}
	return out, nil
}
