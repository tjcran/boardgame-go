// Package typed wraps ccg.Catalog with a generic CardDef[A] so games
// can author card templates whose Attrs is a typed struct (or any
// JSON-object-shaped type) instead of map[string]any. The underlying
// untyped *ccg.Catalog is reachable via Untyped() and consumed by
// State.Instantiate / State.LoadDeckList / DeckPile etc. unchanged —
// the runtime entity still stores its attrs in Entity.Attrs, just
// flattened from the def's typed shape at Register time.
//
// Generics live in this sub-package so the existing ccg surface
// remains un-typed: importing ccg costs nothing extra for games that
// don't want generics, and adopting typed.CardDef is a strictly
// additive layer on top.
package typed

import (
	"encoding/json"
	"errors"

	"github.com/tjcran/boardgame-go/modules/ccg"
)

// CardDef is the typed mirror of ccg.CardDef. Attrs may be any type
// whose JSON marshal output is a JSON object — typically a struct
// with json-tagged fields, or a map[string]X. Primitive A types
// (int / string / bool) are rejected at Register time because their
// JSON form isn't an object and can't be flattened into the entity's
// Attrs map.
type CardDef[A any] struct {
	ID    ccg.DefID
	Type  string
	Attrs A
}

// Catalog wraps a *ccg.Catalog with a typed lookup table keyed by
// the same DefID. Register records the typed def for later Get
// lookups and forwards a flattened copy of Attrs to the wrapped
// untyped catalog so State.Instantiate produces entities with the
// def's attrs intact.
type Catalog[A any] struct {
	untyped *ccg.Catalog
	typed   map[ccg.DefID]CardDef[A]
}

// ErrNonObjectAttrs is returned by Register when the typed Attrs
// can't be flattened to a JSON object (e.g. A is a primitive).
var ErrNonObjectAttrs = errors.New("ccg/typed: Attrs must JSON-marshal to an object")

// NewCatalog builds an empty typed catalog with a fresh underlying
// untyped ccg.Catalog.
func NewCatalog[A any]() *Catalog[A] {
	return &Catalog[A]{
		untyped: ccg.NewCatalog(),
		typed:   map[ccg.DefID]CardDef[A]{},
	}
}

// Register records the typed def and forwards a flattened copy to
// the underlying untyped catalog. Returns ErrNonObjectAttrs when
// Attrs cannot be marshalled to a JSON object, and propagates
// ccg.ErrDuplicateDef when the DefID is already registered. On a
// duplicate or flatten failure, neither the typed nor the untyped
// catalog is mutated.
func (c *Catalog[A]) Register(def CardDef[A]) error {
	attrsMap, err := flattenAttrs(def.Attrs)
	if err != nil {
		return err
	}
	if err := c.untyped.Register(ccg.CardDef{
		ID:        def.ID,
		Type:      def.Type,
		BaseAttrs: attrsMap,
	}); err != nil {
		return err
	}
	c.typed[def.ID] = def
	return nil
}

// Get returns the typed def for an ID, plus a found bool.
func (c *Catalog[A]) Get(id ccg.DefID) (CardDef[A], bool) {
	d, ok := c.typed[id]
	return d, ok
}

// Len returns the number of registered defs.
func (c *Catalog[A]) Len() int { return len(c.typed) }

// Untyped exposes the underlying ccg.Catalog. Pass this to
// State.Instantiate / State.LoadDeckList / DeckPile etc.
//
//	id, _ := state.Instantiate(cat.Untyped(), "goblin", "0")
func (c *Catalog[A]) Untyped() *ccg.Catalog { return c.untyped }

// Get returns the typed CardDef for a runtime entity by looking up
// the entity's stored DefID in the catalog. Returns (zero, false)
// when the entity is unknown, has no DefID (was minted via
// NewEntity rather than Instantiate), or carries a DefID that isn't
// registered in this typed catalog.
//
// Note: the returned CardDef carries the *printed* attrs (def
// baseline). Per-instance mutations to Entity.Attrs are not
// reflected — use ccg.State.EffectiveAttr for live values that
// respect modifiers.
func Get[A any](s *ccg.State, c *Catalog[A], id ccg.EntityID) (CardDef[A], bool) {
	e, ok := s.Get(id)
	if !ok || e.DefID == "" {
		return CardDef[A]{}, false
	}
	return c.Get(e.DefID)
}

// flattenAttrs round-trips attrs through JSON into map[string]any.
// Returns ErrNonObjectAttrs when the JSON form isn't an object.
// Numbers come back as float64; ccg.Entity.AttrInt already handles
// that case.
func flattenAttrs(v any) (map[string]any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	if len(b) == 0 || b[0] != '{' {
		return nil, ErrNonObjectAttrs
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return m, nil
}
