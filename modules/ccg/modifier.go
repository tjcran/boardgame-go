package ccg

import "sort"

// ModifierID is the stable handle for a modifier.
type ModifierID uint64

// ModifierOp describes how the modifier combines with the attribute's
// base value. Use Set to overwrite, Add to delta, Multiply to scale.
// More complex math (capped damage, percentage bonuses, etc.) is
// game-specific — apps can implement bespoke ops by writing their own
// recalc on top of EffectiveAttr's output.
type ModifierOp string

const (
	OpSet      ModifierOp = "set"
	OpAdd      ModifierOp = "add"
	OpMultiply ModifierOp = "mul"
)

// Modifier is one active rule changing an entity's effective attribute.
// Apply order: ascending Layer first, then insertion order within a
// layer. Modifiers at Layer 0 (the default) all behave as a single
// "flat" layer applied insertion-order — the simple model is the
// special case of the layered model.
//
// The MTG layer convention (1=copy, 2=control, 3=text-changing,
// 4=type, 5=color, 6=ability, 7a-d=P/T sub-layers) is one common
// mapping but the library doesn't enforce it; pick whatever ascending
// integer layout suits your game.
type Modifier struct {
	ID        ModifierID `json:"id"`
	Source    EntityID   `json:"source,omitempty"`
	Layer     int        `json:"layer,omitempty"`
	Target    EntityID   `json:"target,omitempty"`
	Attribute string     `json:"attribute"`
	Op        ModifierOp `json:"op"`
	Value     any        `json:"value,omitempty"`
	// Note  is a free-text annotation. Useful for game logs.
	Note string `json:"note,omitempty"`
	// insertion is the order in which the modifier was added.
	// Persisted so reloads keep stable ordering.
	Insertion int `json:"insertion"`
}

// AddModifier registers a new modifier and returns its ID. The state
// tracks insertion order so recalculation is reproducible after a
// JSON round trip.
func (s *State) AddModifier(m Modifier) ModifierID {
	s.nextModID++
	m.ID = ModifierID(s.nextModID)
	m.Insertion = s.nextInsertion
	s.nextInsertion++
	if s.Modifiers == nil {
		s.Modifiers = map[ModifierID]Modifier{}
	}
	s.Modifiers[m.ID] = m
	return m.ID
}

// RemoveModifier drops a modifier from the state. No-op when the id
// is unknown.
func (s *State) RemoveModifier(id ModifierID) {
	delete(s.Modifiers, id)
}

// EffectiveAttr returns the post-modifier value for attr on entity.
// def is returned when neither the entity nor any modifier supplies a
// value. Layered semantics:
//
//   1. Start with def (or the entity's base Attrs[attr] when present).
//   2. Walk all modifiers targeting this entity (Target == id) and the
//      named attribute, sorted by (Layer, Insertion).
//   3. Apply each modifier: Set replaces, Add increments, Multiply
//      scales.
//   4. Return the result.
//
// Apps that need different semantics (capped values, cleared by
// effect, etc.) can read the modifier set via Modifiers and roll
// their own — EffectiveAttr is a default, not a mandate.
func (s *State) EffectiveAttr(id EntityID, attr string, def any) any {
	value := def
	if e, ok := s.Entities[id]; ok && e.Attrs != nil {
		if v, ok := e.Attrs[attr]; ok {
			value = v
		}
	}
	mods := s.modifiersFor(id, attr)
	for _, m := range mods {
		value = applyOp(value, m)
	}
	return value
}

// modifiersFor returns every modifier targeting (entity, attr) in the
// right apply order. Internal.
func (s *State) modifiersFor(id EntityID, attr string) []Modifier {
	var out []Modifier
	for _, m := range s.Modifiers {
		if m.Target == id && m.Attribute == attr {
			out = append(out, m)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Layer != out[j].Layer {
			return out[i].Layer < out[j].Layer
		}
		return out[i].Insertion < out[j].Insertion
	})
	return out
}

// applyOp folds one modifier into the running value. Numeric math
// falls through to int64 / float64 depending on operand types so
// the user doesn't have to think about it.
func applyOp(cur any, m Modifier) any {
	switch m.Op {
	case OpSet:
		return m.Value
	case OpAdd:
		return numericAdd(cur, m.Value)
	case OpMultiply:
		return numericMul(cur, m.Value)
	}
	return cur
}

func numericAdd(a, b any) any {
	af, aok := toFloat(a)
	bf, bok := toFloat(b)
	if !aok || !bok {
		return a
	}
	// Preserve int-ness when both inputs are integers.
	if _, aint := asInt(a); aint {
		if _, bint := asInt(b); bint {
			return int(af + bf)
		}
	}
	return af + bf
}

func numericMul(a, b any) any {
	af, aok := toFloat(a)
	bf, bok := toFloat(b)
	if !aok || !bok {
		return a
	}
	if _, aint := asInt(a); aint {
		if _, bint := asInt(b); bint {
			return int(af * bf)
		}
	}
	return af * bf
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case float64:
		return n, true
	case float32:
		return float64(n), true
	}
	return 0, false
}

func asInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	}
	return 0, false
}
