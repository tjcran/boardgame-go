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

// IsVisibleTo reports whether the entity is visible to the given
// viewer under its current Visibility setting.
func (e Entity) IsVisibleTo(viewer string) bool {
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
//   - OwnerOnly entities whose Owner != viewer, and Hidden entities:
//     collapsed to {ID, Zone, Visibility} stubs. Type / Owner / Attrs /
//     DefID are stripped so neither identity nor stats leak.
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
	out.nextEntityID = s.nextEntityID
	out.nextModID = s.nextModID
	out.nextInsertion = s.nextInsertion
	out.nextSubID = s.nextSubID

	redacted := map[EntityID]bool{}
	for id, e := range s.Entities {
		if e.IsVisibleTo(viewer) {
			out.Entities[id] = Entity{
				ID:         e.ID,
				DefID:      e.DefID,
				Type:       e.Type,
				Owner:      e.Owner,
				Zone:       e.Zone,
				Visibility: e.Visibility,
				Attrs:      cloneAttrs(e.Attrs),
			}
		} else {
			redacted[id] = true
			out.Entities[id] = Entity{
				ID:         e.ID,
				Zone:       e.Zone,
				Visibility: e.Visibility,
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
