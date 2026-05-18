package ccg

// TargetQuery is the builder for "find all entities matching X." Chain
// predicates and call Find / First to materialise the result. Predicates
// AND together; for OR, run two queries and merge.
//
// Each predicate is a small closure over the current State + Entity;
// builds are cheap, queries run linearly over State.Entities so apps
// with very large entity counts may want to pre-index by zone /
// type / etc. The library favours the small-N case (typical CCG: a
// few dozen entities live).
type TargetQuery struct {
	state      *State
	predicates []func(Entity) bool
}

// Query starts a new target query over state.
func Query(state *State) *TargetQuery { return &TargetQuery{state: state} }

// InZone restricts to entities currently in the named zone.
func (q *TargetQuery) InZone(name ZoneName) *TargetQuery {
	q.predicates = append(q.predicates, func(e Entity) bool { return e.Zone == name })
	return q
}

// Controlled restricts to entities owned by the given player.
func (q *TargetQuery) Controlled(playerID string) *TargetQuery {
	q.predicates = append(q.predicates, func(e Entity) bool { return e.Owner == playerID })
	return q
}

// NotControlled restricts to entities NOT owned by the given player.
// Handy for "all opponent's creatures" filters.
func (q *TargetQuery) NotControlled(playerID string) *TargetQuery {
	q.predicates = append(q.predicates, func(e Entity) bool { return e.Owner != playerID })
	return q
}

// HasType restricts to entities with the given Type tag.
func (q *TargetQuery) HasType(t string) *TargetQuery {
	q.predicates = append(q.predicates, func(e Entity) bool { return e.Type == t })
	return q
}

// AttrAtLeast restricts to entities whose attribute is >= min. Uses
// EffectiveAttr so modifiers count.
func (q *TargetQuery) AttrAtLeast(attr string, min int) *TargetQuery {
	state := q.state
	q.predicates = append(q.predicates, func(e Entity) bool {
		v := state.EffectiveAttr(e.ID, attr, 0)
		switch n := v.(type) {
		case int:
			return n >= min
		case int64:
			return n >= int64(min)
		case float64:
			return n >= float64(min)
		}
		return false
	})
	return q
}

// AttrAtMost is the symmetric ≤ filter.
func (q *TargetQuery) AttrAtMost(attr string, max int) *TargetQuery {
	state := q.state
	q.predicates = append(q.predicates, func(e Entity) bool {
		v := state.EffectiveAttr(e.ID, attr, 0)
		switch n := v.(type) {
		case int:
			return n <= max
		case int64:
			return n <= int64(max)
		case float64:
			return n <= float64(max)
		}
		return false
	})
	return q
}

// Where adds an arbitrary predicate. Use this for filters the builder
// doesn't have a method for.
func (q *TargetQuery) Where(pred func(Entity) bool) *TargetQuery {
	q.predicates = append(q.predicates, pred)
	return q
}

// Find returns every entity matching every predicate. Order is
// deterministic (sorted by EntityID ascending) so replays produce
// equal slices.
func (q *TargetQuery) Find() []EntityID {
	if q.state == nil {
		return nil
	}
	var ids []EntityID
	for id, e := range q.state.Entities {
		ok := true
		for _, p := range q.predicates {
			if !p(e) {
				ok = false
				break
			}
		}
		if ok {
			ids = append(ids, id)
		}
	}
	sortIDs(ids)
	return ids
}

// First returns the first matching entity ID or 0 when none match.
func (q *TargetQuery) First() EntityID {
	ids := q.Find()
	if len(ids) == 0 {
		return 0
	}
	return ids[0]
}

// Count is a shortcut for len(Find()) that avoids the slice allocation.
func (q *TargetQuery) Count() int {
	if q.state == nil {
		return 0
	}
	n := 0
	for _, e := range q.state.Entities {
		ok := true
		for _, p := range q.predicates {
			if !p(e) {
				ok = false
				break
			}
		}
		if ok {
			n++
		}
	}
	return n
}

// Filter is the free-function form for one-off filters that don't
// merit a TargetQuery builder. Iterates the entity map (random order)
// — sort the result if you need determinism.
func Filter(entities map[EntityID]Entity, pred func(Entity) bool) []EntityID {
	var out []EntityID
	for id, e := range entities {
		if pred(e) {
			out = append(out, id)
		}
	}
	sortIDs(out)
	return out
}

// sortIDs sorts in place ascending.
func sortIDs(ids []EntityID) {
	// Tiny insertion sort — entity sets are small in practice; saves
	// us pulling in sort just for this.
	for i := 1; i < len(ids); i++ {
		v := ids[i]
		j := i
		for j > 0 && ids[j-1] > v {
			ids[j] = ids[j-1]
			j--
		}
		ids[j] = v
	}
}
