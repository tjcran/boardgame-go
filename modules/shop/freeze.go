package shop

import "github.com/tjcran/boardgame-go/modules/ccg"

// Freeze marks an entity in Slots to survive the next Clear / Roll.
// Returns ErrNotInSlots when the entity is not currently in Slots.
// Idempotent — re-freezing is a no-op.
func (sh Shop) Freeze(s *ccg.State, id ccg.EntityID) error {
	if !s.Contains(sh.Slots, id) {
		return ErrNotInSlots
	}
	s.SetAttr(id, ShopFrozenAttrKey, true)
	return nil
}

// Unfreeze clears the freeze flag. No-op when the entity is unfrozen
// or unknown. Does NOT require the entity to be in Slots — Unfreeze on
// a sold (now-in-hand) item is fine.
func (sh Shop) Unfreeze(s *ccg.State, id ccg.EntityID) error {
	e, ok := s.Get(id)
	if !ok {
		return nil
	}
	if e.Attrs == nil {
		return nil
	}
	if _, exists := e.Attrs[ShopFrozenAttrKey]; exists {
		delete(e.Attrs, ShopFrozenAttrKey)
		// Re-store the entity since Entity is value-typed in the map.
		// (Mirrors ccg.SetAttr's pattern.)
		s.Entities[id] = e
	}
	return nil
}

// IsFrozen reports whether the entity carries the freeze flag.
// Returns false for unknown entities.
func (sh Shop) IsFrozen(s *ccg.State, id ccg.EntityID) bool {
	e, ok := s.Get(id)
	if !ok {
		return false
	}
	if e.Attrs == nil {
		return false
	}
	v, ok := e.Attrs[ShopFrozenAttrKey]
	if !ok {
		return false
	}
	b, _ := v.(bool)
	return b
}
