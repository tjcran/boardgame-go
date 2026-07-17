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
	Entities       map[EntityID]Entity     `json:"entities,omitempty"`
	Zones          map[ZoneName]*Zone      `json:"zones,omitempty"`
	Modifiers      map[ModifierID]Modifier `json:"modifiers,omitempty"`
	Events         []Event                 `json:"events,omitempty"`
	PendingEffects []Effect                `json:"pending_effects,omitempty"`

	// StagedTriggers is the holding buffer for effects staged by event
	// handlers between trigger checkpoints — see StageTrigger /
	// FlushTriggers in trigger.go. Serialised (a match can persist
	// mid-checkpoint); IDs are zero until flush.
	StagedTriggers []Effect `json:"staged_triggers,omitempty"`

	// nextEntityID / nextModID / nextSubID / nextAbilityID /
	// nextEffectID are monotonic counters for stable IDs. Replay-safe:
	// equal Setup + equal moves → equal IDs.
	nextEntityID  uint64
	nextModID     uint64
	nextInsertion int
	nextSubID     uint64
	nextAbilityID uint64
	nextEffectID  uint64

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
		if m.Target == id || m.Source == id || m.ExpiresWith == id {
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

// SetController updates who controls the entity; empty restores
// owner control. No-op when the entity is unknown. Convenience
// mirror of SetAttr — Entity is value-typed so mutations must be
// written back into the map.
func (s *State) SetController(id EntityID, playerID string) {
	e, ok := s.Entities[id]
	if !ok {
		return
	}
	e.Controller = playerID
	s.Entities[id] = e
}

// Clone mints a new entity copying the source's template identity:
// Type, DefID, Visibility, and a deep copy of its current base Attrs
// with overrides applied on top. What it deliberately does NOT copy:
// zone placement (the clone starts unplaced — Add it where you want
// it), modifiers (effective values are not template identity),
// counters (the reserved CountersAttrKey entry is stripped), and
// Controller (empty, so the new owner controls it). Returns
// ErrUnknownEntity when the source is missing.
func (s *State) Clone(id EntityID, owner string, overrides map[string]any) (EntityID, error) {
	src, ok := s.Entities[id]
	if !ok {
		return 0, ErrUnknownEntity
	}
	attrs := deepCloneAttrs(src.Attrs)
	delete(attrs, CountersAttrKey)
	for k, v := range overrides {
		if attrs == nil {
			attrs = map[string]any{}
		}
		attrs[k] = v
	}
	s.nextEntityID++
	nid := EntityID(s.nextEntityID)
	s.Entities[nid] = Entity{
		ID:         nid,
		DefID:      src.DefID,
		Type:       src.Type,
		Owner:      owner,
		Visibility: src.Visibility,
		Attrs:      attrs,
	}
	return nid, nil
}

// deepCloneAttrs copies an attribute map including nested maps and
// slices, so a clone's attrs never alias the source's. Scalars are
// copied by value; unrecognized reference types pass through as-is.
func deepCloneAttrs(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = deepCloneValue(v)
	}
	return out
}

func deepCloneValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		return deepCloneAttrs(t)
	case map[string]int:
		out := make(map[string]int, len(t))
		for k, n := range t {
			out[k] = n
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = deepCloneValue(e)
		}
		return out
	}
	return v
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
