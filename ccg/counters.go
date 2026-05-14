package ccg

import "errors"

// CountersAttrKey is the reserved Entity.Attrs key under which the
// counter API stores per-entity counter totals as map[string]int.
// Games should not write to this key directly — use AddCounter /
// RemoveCounter / TransferCounters instead.
const CountersAttrKey = "__counters"

// EventCounterChanged is the Event.Type published by every counter
// mutation. The payload is keyed under Event.Data:
//
//   kind         string  — counter kind (e.g. "+1/+1", "loyalty")
//   delta        int     — applied signed change (may differ from the
//                          caller's argument when the floor at 0 kicks
//                          in; never zero in published events)
//   total_after  int     — post-mutation total for `kind` on Source
//
// TransferCounters publishes two events: one with Source = from and
// negative delta, one with Source = to and positive delta. Subscribers
// that only care about gains or only losses filter on sign of delta.
const EventCounterChanged = "counter_changed"

// ErrInsufficientCounters is returned by TransferCounters when the
// source entity holds fewer counters of the requested kind than the
// transfer amount.
var ErrInsufficientCounters = errors.New("ccg: insufficient counters")

// AddCounter adjusts the count of `kind` counters on `id` by n. n may
// be negative; the resulting total clamps at 0 (counter counts never
// go negative). No-op when the entity is unknown or when the applied
// delta would be zero — in which case no event fires.
//
// Publishes EventCounterChanged with the *applied* delta (which may
// differ from n when the floor at 0 kicks in) and the post-mutation
// total. A counter whose new total is 0 is removed from the entity's
// counter map; reading it returns 0 either way.
func (s *State) AddCounter(id EntityID, kind string, n int) {
	s.changeCounter(id, kind, n)
}

// RemoveCounter is sugar for AddCounter(id, kind, -n). n is the
// positive count to remove; a negative n is treated as 0 (no-op).
func (s *State) RemoveCounter(id EntityID, kind string, n int) {
	if n < 0 {
		return
	}
	s.changeCounter(id, kind, -n)
}

// Counters returns the current count of `kind` on `id`. Returns 0 for
// unknown entities or unset counters.
func (s *State) Counters(id EntityID, kind string) int {
	e, ok := s.Entities[id]
	if !ok {
		return 0
	}
	return readCountersFromAttrs(e.Attrs)[kind]
}

// AllCounters returns a copy of every counter on `id` (kind → count).
// Returns nil for unknown entities or entities with no counters.
// Mutating the returned map does not affect state.
func (s *State) AllCounters(id EntityID) map[string]int {
	e, ok := s.Entities[id]
	if !ok {
		return nil
	}
	m := readCountersFromAttrs(e.Attrs)
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]int, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// TransferCounters moves n counters of `kind` from `from` to `to`.
// Pre-flight all-or-nothing: validates both entities exist and that
// `from` has at least n counters of `kind` before mutating either
// side. Returns ErrUnknownEntity or ErrInsufficientCounters and
// leaves state untouched on error. n ≤ 0 and self-transfers are
// no-ops returning nil.
//
// Publishes two EventCounterChanged events on success.
func (s *State) TransferCounters(from, to EntityID, kind string, n int) error {
	if n <= 0 {
		return nil
	}
	fe, ok := s.Entities[from]
	if !ok {
		return ErrUnknownEntity
	}
	if _, ok := s.Entities[to]; !ok {
		return ErrUnknownEntity
	}
	if from == to {
		return nil
	}
	if readCountersFromAttrs(fe.Attrs)[kind] < n {
		return ErrInsufficientCounters
	}
	s.changeCounter(from, kind, -n)
	s.changeCounter(to, kind, n)
	return nil
}

// changeCounter is the single mutation path for counter state.
// Reads-modifies-writes the entity's counter sub-map under
// CountersAttrKey, canonicalising it to map[string]int (the JSON
// unmarshal form is map[string]any; this re-stamps on first write
// after a state reload).
func (s *State) changeCounter(id EntityID, kind string, signedDelta int) {
	e, ok := s.Entities[id]
	if !ok {
		return
	}
	current := readCountersFromAttrs(e.Attrs)
	if current == nil {
		current = map[string]int{}
	}
	before := current[kind]
	after := before + signedDelta
	if after < 0 {
		after = 0
	}
	applied := after - before
	if applied == 0 {
		return
	}
	if after == 0 {
		delete(current, kind)
	} else {
		current[kind] = after
	}
	if e.Attrs == nil {
		e.Attrs = map[string]any{}
	}
	if len(current) == 0 {
		delete(e.Attrs, CountersAttrKey)
	} else {
		e.Attrs[CountersAttrKey] = current
	}
	s.Entities[id] = e
	s.Publish(Event{
		Type:   EventCounterChanged,
		Source: id,
		Data: map[string]any{
			"kind":        kind,
			"delta":       applied,
			"total_after": after,
		},
	})
}

// readCountersFromAttrs extracts the counter sub-map from an entity's
// Attrs, coercing from map[string]any (the form a JSON-round-tripped
// State produces) when needed. Returns nil when no counters are set.
func readCountersFromAttrs(attrs map[string]any) map[string]int {
	if attrs == nil {
		return nil
	}
	raw, ok := attrs[CountersAttrKey]
	if !ok {
		return nil
	}
	switch m := raw.(type) {
	case map[string]int:
		return m
	case map[string]any:
		out := make(map[string]int, len(m))
		for k, v := range m {
			if n, ok := counterToInt(v); ok {
				out[k] = n
			}
		}
		return out
	}
	return nil
}

func counterToInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	}
	return 0, false
}
