package ccg

// EntityID is the stable handle for an entity across its lifetime. IDs
// are monotonically assigned by State.NewEntity and survive zone moves,
// modifier attachment, serialisation — anything except explicit
// Destroy. Replay-safe: equal Setup input + equal moves -> equal IDs.
type EntityID uint64

// Entity is the bookkeeping shape for a card, token, permanent, or any
// other addressable game object. The library is opinion-free about
// what "Type" means: pick "creature" / "spell" / "artifact" / "tile"
// or anything else.
//
// Owner is the playerID who owns the entity (typically distinct from
// who controls it — see Modifier for control changes). Empty Owner is
// a neutral / untracked entity.
//
// Attrs is the game-author's escape hatch: arbitrary keyed values
// the library passes through unmodified but uses for target queries
// and modifier targeting. Helper accessors AttrInt / AttrStr /
// AttrBool give type-asserting reads with a default-on-missing.
type Entity struct {
	ID    EntityID       `json:"id"`
	Type  string         `json:"type,omitempty"`
	Owner string         `json:"owner,omitempty"`
	Zone  ZoneName       `json:"zone,omitempty"`
	Attrs map[string]any `json:"attrs,omitempty"`
}

// AttrInt returns the int-cast attr value or def when missing /
// non-numeric. JSON unmarshalling produces float64 for numbers, which
// is handled.
func (e Entity) AttrInt(key string, def int) int {
	if e.Attrs == nil {
		return def
	}
	switch v := e.Attrs[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	}
	return def
}

// AttrStr returns the string attr value or def when missing.
func (e Entity) AttrStr(key, def string) string {
	if e.Attrs == nil {
		return def
	}
	if s, ok := e.Attrs[key].(string); ok {
		return s
	}
	return def
}

// AttrBool returns the bool attr value or def when missing.
func (e Entity) AttrBool(key string, def bool) bool {
	if e.Attrs == nil {
		return def
	}
	if b, ok := e.Attrs[key].(bool); ok {
		return b
	}
	return def
}

// SetAttr is the canonical mutator. Direct map writes work too but
// going through this helper guarantees the map is initialised.
func (e *Entity) SetAttr(key string, value any) {
	if e.Attrs == nil {
		e.Attrs = map[string]any{}
	}
	e.Attrs[key] = value
}
