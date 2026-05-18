package ccg

import "errors"

// DefID is the stable key of a card template in a Catalog. Pick any
// string scheme that makes sense for the game ("goblin_01", "M21:42").
// The library treats it as opaque and is replay-safe so long as the
// catalog registration order is deterministic.
type DefID string

// CardDef is the printed face of a card — the static template that
// gets stamped onto fresh Entities at deal-out time. Game authors
// register CardDefs into a Catalog during setup, then call
// State.Instantiate(catalog, defID, owner) to mint runtime entities.
//
// BaseAttrs are cloned into every instantiated Entity; later mutations
// to an Entity's Attrs do not leak back into the def.
type CardDef struct {
	ID        DefID          `json:"id"`
	Type      string         `json:"type,omitempty"`
	BaseAttrs map[string]any `json:"base_attrs,omitempty"`
}

// Catalog is an append-only registry of CardDef templates keyed by
// DefID. Re-registering an existing DefID returns ErrDuplicateDef
// rather than silently overwriting — replay safety depends on the
// catalog's contents not drifting under a running game.
type Catalog struct {
	defs map[DefID]CardDef
}

// ErrDuplicateDef / ErrUnknownDef are returned by Catalog and
// State.Instantiate for the obvious failure modes.
var (
	ErrDuplicateDef = errors.New("ccg: duplicate card def")
	ErrUnknownDef   = errors.New("ccg: unknown card def")
)

// NewCatalog builds an empty Catalog.
func NewCatalog() *Catalog {
	return &Catalog{defs: map[DefID]CardDef{}}
}

// Register adds a CardDef. Returns ErrDuplicateDef if the ID is
// already registered.
func (c *Catalog) Register(def CardDef) error {
	if _, ok := c.defs[def.ID]; ok {
		return ErrDuplicateDef
	}
	c.defs[def.ID] = CardDef{
		ID:        def.ID,
		Type:      def.Type,
		BaseAttrs: cloneAttrs(def.BaseAttrs),
	}
	return nil
}

// Get returns the CardDef for an ID, plus a found bool.
func (c *Catalog) Get(id DefID) (CardDef, bool) {
	d, ok := c.defs[id]
	return d, ok
}

// Len returns the number of registered defs.
func (c *Catalog) Len() int {
	return len(c.defs)
}
