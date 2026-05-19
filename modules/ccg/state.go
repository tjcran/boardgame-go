package ccg

// State is the bookkeeping container — entities, zones, modifiers,
// recent events. Game authors embed *State (or a value) into their G:
//
//	type MyG struct {
//	    *ccg.State
//	    HP map[string]int
//	}
//
// The library mutates State in place; the engine's NewMatch /
// Apply / Replay all work because State is just a JSON-friendly Go
// struct.
//
// Subscribers are intentionally NOT in State — they don't serialise
// and must be re-registered every process start. Persisted state
// (Entities, Zones, Modifiers, Events) survives a server bounce
// cleanly; the routing table is rebuilt by the game's NewMatch
// equivalent.
type State struct {
	Entities  map[EntityID]Entity      `json:"entities,omitempty"`
	Zones     map[ZoneName]*Zone       `json:"zones,omitempty"`
	Modifiers map[ModifierID]Modifier  `json:"modifiers,omitempty"`
	Events    []Event                  `json:"events,omitempty"`

	// nextEntityID / nextModID / nextSubID / nextAbilityID are
	// monotonic counters for stable IDs. Replay-safe: equal Setup +
	// equal moves → equal IDs.
	nextEntityID   uint64
	nextModID      uint64
	nextInsertion  int
	nextSubID      uint64
	nextAbilityID  uint64

	// subs / abilities are in-memory tables (not serialised). The
	// engine re-registers them at process start the same way it
	// re-registers subs.
	subs      []subscription
	abilities []ability
}

// NewState builds an empty State.
func NewState() *State {
	return &State{
		Entities:  map[EntityID]Entity{},
		Zones:     map[ZoneName]*Zone{},
		Modifiers: map[ModifierID]Modifier{},
	}
}

// NewEntity registers a fresh entity with a stable ID. Owner can be
// empty for neutral entities; Type and Attrs are entirely
// game-defined. Returns the assigned EntityID.
func (s *State) NewEntity(typeName, owner string, attrs map[string]any) EntityID {
	s.nextEntityID++
	id := EntityID(s.nextEntityID)
	s.Entities[id] = Entity{
		ID: id, Type: typeName, Owner: owner, Attrs: cloneAttrs(attrs),
	}
	return id
}

// Instantiate mints a fresh Entity stamped from a CardDef registered
// in the given Catalog. The new entity's Type and Attrs are copied
// from the def (BaseAttrs is cloned, so later per-instance attr
// mutations don't leak back), and Entity.DefID is set to the def's ID
// so the runtime entity can be linked back to its template.
//
// Returns ErrUnknownDef when the def is not registered in the catalog.
// A nil catalog is treated as an empty one (always returns
// ErrUnknownDef).
func (s *State) Instantiate(c *Catalog, def DefID, owner string) (EntityID, error) {
	if c == nil {
		return 0, ErrUnknownDef
	}
	d, ok := c.Get(def)
	if !ok {
		return 0, ErrUnknownDef
	}
	s.nextEntityID++
	id := EntityID(s.nextEntityID)
	s.Entities[id] = Entity{
		ID:    id,
		DefID: d.ID,
		Type:  d.Type,
		Owner: owner,
		Attrs: cloneAttrs(d.BaseAttrs),
	}
	return id, nil
}

// Destroy removes an entity from state — including its current zone
// and any modifiers targeting it. Use with care; most CCGs move
// entities through zones rather than destroying them.
func (s *State) Destroy(id EntityID) {
	e, ok := s.Entities[id]
	if !ok {
		return
	}
	if e.Zone != "" {
		if z, ok := s.Zones[e.Zone]; ok {
			for i, m := range z.Members {
				if m == id {
					z.Members = append(z.Members[:i], z.Members[i+1:]...)
					break
				}
			}
		}
	}
	for mid, m := range s.Modifiers {
		if m.Target == id || m.Source == id {
			delete(s.Modifiers, mid)
		}
	}
	delete(s.Entities, id)
}

// Get returns the entity by ID or the zero value when missing. The
// second return is the existence bool — use it to distinguish a
// missing entity from a zero-valued one.
func (s *State) Get(id EntityID) (Entity, bool) {
	e, ok := s.Entities[id]
	return e, ok
}

// SetAttr is a convenience for state.Entities[id].SetAttr; persists
// the mutation back into the map (Entity is value-typed).
func (s *State) SetAttr(id EntityID, key string, value any) {
	e, ok := s.Entities[id]
	if !ok {
		return
	}
	e.SetAttr(key, value)
	s.Entities[id] = e
}

// cloneAttrs defensively copies the user-supplied attribute map so
// later mutations to the caller's map don't leak into the entity.
func cloneAttrs(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
