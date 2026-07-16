package ccg

// Visibility controls how an entity appears in the redacted view of a
// State produced by RedactForViewer. The zero value is Public, so
// existing games and pre-existing entities are unaffected.
type Visibility uint8

const (
	// Public — visible to every viewer. Default.
	Public Visibility = iota
	// OwnerOnly — visible only to Entity.Owner; redacted for everyone
	// else. Use for cards in a player's hand.
	OwnerOnly
	// Hidden — visible to no one, including the owner. Use for the
	// face-down deck, exile-face-down, library tutoring without reveal.
	Hidden
)

// SetVisibility updates an entity's visibility. Mirrors SetAttr in
// shape: silent no-op if the entity is unknown, value-typed re-store
// to persist the mutation.
func (s *State) SetVisibility(id EntityID, v Visibility) {
	e, ok := s.Entities[id]
	if !ok {
		return
	}
	e.Visibility = v
	s.Entities[id] = e
}

// RevealTo grants the listed viewers full visibility of the entity
// regardless of its Visibility setting. Duplicate viewers are not
// re-added. No-op when the entity is unknown.
func (s *State) RevealTo(id EntityID, viewers ...string) {
	e, ok := s.Entities[id]
	if !ok {
		return
	}
	for _, v := range viewers {
		seen := false
		for _, have := range e.RevealedTo {
			if have == v {
				seen = true
				break
			}
		}
		if !seen {
			e.RevealedTo = append(e.RevealedTo, v)
		}
	}
	s.Entities[id] = e
}

// ConcealFrom revokes a RevealTo grant. No-op when the entity is
// unknown or the viewer was never revealed to.
func (s *State) ConcealFrom(id EntityID, viewer string) {
	e, ok := s.Entities[id]
	if !ok {
		return
	}
	for i, have := range e.RevealedTo {
		if have == viewer {
			e.RevealedTo = append(e.RevealedTo[:i], e.RevealedTo[i+1:]...)
			break
		}
	}
	s.Entities[id] = e
}

// SetMask replaces the entity's redaction mask (see Entity.Mask).
// Nil clears it. No-op when the entity is unknown.
func (s *State) SetMask(id EntityID, mask map[string]any) {
	e, ok := s.Entities[id]
	if !ok {
		return
	}
	e.Mask = cloneAttrs(mask)
	s.Entities[id] = e
}

// IsVisibleTo reports whether the entity is visible to the given
// viewer under its current Visibility setting. A RevealTo grant
// overrides Visibility entirely — including Hidden.
func (e Entity) IsVisibleTo(viewer string) bool {
	for _, v := range e.RevealedTo {
		if v == viewer {
			return true
		}
	}
	switch e.Visibility {
	case Public:
		return true
	case OwnerOnly:
		return e.Owner == viewer
	case Hidden:
		return false
	default:
		// Unknown visibility values default to visible. Future
		// variants (e.g. team-visible) should be added as explicit
		// cases rather than relying on this default.
		return true
	}
}

// RedactForViewer returns a deep copy of the state with entities,
// modifiers, and events filtered to what the named viewer is allowed
// to see:
//
//   - Public entities: copied verbatim.
//   - OwnerOnly entities whose Owner == viewer: copied verbatim.
//   - Entities whose RevealedTo lists the viewer: copied verbatim,
//     whatever their Visibility.
//   - Everything else (OwnerOnly for other viewers, Hidden):
//     collapsed to {ID, Zone, Visibility} stubs. Type / Owner / Attrs /
//     DefID are stripped so neither identity nor stats leak. When the
//     entity has a Mask, the stub's Attrs carry the mask content —
//     the game-chosen "what non-viewers are told".
//   - Zones: copied with their Members slice intact, so positional
//     metadata ("third from the top of the deck has ID 42") is
//     preserved by ID — games that don't want this should shuffle
//     before redacting.
//   - Modifiers whose Source or Target references a redacted entity:
//     reduced to {ID, Layer, Insertion} stubs (the layered ordering is
//     preserved for the visible modifiers around them; the actual
//     attr change is stripped).
//   - Events whose Source or Target references a redacted entity:
//     dropped from the buffer. Apps that need fine-grained event
//     filtering should keep their own audit log.
//
// The returned State is a fresh allocation; mutating it does not
// affect the original. The viewer string is matched against
// Entity.Owner — pass "" to redact every OwnerOnly entity (e.g.,
// when serialising for spectators).
func (s *State) RedactForViewer(viewer string) *State {
	out := NewState()
	out.IDs.NextEntityID = s.IDs.NextEntityID
	out.IDs.NextModID = s.IDs.NextModID
	out.IDs.NextInsertion = s.IDs.NextInsertion
	out.IDs.NextSubID = s.IDs.NextSubID

	redacted := map[EntityID]bool{}
	for id, e := range s.Entities {
		if e.IsVisibleTo(viewer) {
			out.Entities[id] = Entity{
				ID:         e.ID,
				DefID:      e.DefID,
				Type:       e.Type,
				Owner:      e.Owner,
				Controller: e.Controller,
				Zone:       e.Zone,
				Visibility: e.Visibility,
				RevealedTo: append([]string(nil), e.RevealedTo...),
				Mask:       cloneAttrs(e.Mask),
				Attrs:      cloneAttrs(e.Attrs),
			}
		} else {
			redacted[id] = true
			// The stub carries the entity's Mask as its visible Attrs
			// — "what non-viewers are told" — deep-copied so a client
			// mutating its view can't reach back into the real state.
			out.Entities[id] = Entity{
				ID:         e.ID,
				Zone:       e.Zone,
				Visibility: e.Visibility,
				Attrs:      deepCloneAttrs(e.Mask),
			}
		}
	}

	for name, z := range s.Zones {
		members := make([]EntityID, len(z.Members))
		copy(members, z.Members)
		out.Zones[name] = &Zone{
			Name:     z.Name,
			Ordered:  z.Ordered,
			Capacity: z.Capacity,
			Members:  members,
		}
	}

	for mid, m := range s.Modifiers {
		if redacted[m.Source] || redacted[m.Target] {
			out.Modifiers[mid] = Modifier{
				ID:        m.ID,
				Layer:     m.Layer,
				Insertion: m.Insertion,
			}
		} else {
			out.Modifiers[mid] = Modifier{
				ID:        m.ID,
				Source:    m.Source,
				Layer:     m.Layer,
				Target:    m.Target,
				Attribute: m.Attribute,
				Op:        m.Op,
				Value:     m.Value,
				Note:      m.Note,
				Insertion: m.Insertion,
			}
		}
	}

	for _, ev := range s.Events {
		if redacted[ev.Source] || redacted[ev.Target] {
			continue
		}
		out.Events = append(out.Events, Event{
			Type:   ev.Type,
			Source: ev.Source,
			Target: ev.Target,
			Data:   cloneAttrs(ev.Data),
		})
	}

	return out
}
