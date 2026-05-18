package ccg

// AbilityID is the stable handle for a lifecycle-managed event
// subscription created via BindAbility.
type AbilityID uint64

// ability is the internal record of a bound ability. Not persisted —
// games re-bind during their normal "rebuild subscribers on process
// start" walk, the same model as subscription.
type ability struct {
	id     AbilityID
	entity EntityID
	zones  []ZoneName
	subID  SubscriptionID
}

// BindAbility wires a handler to fire for events matching predicate
// while the named entity sits in any of the listed zones. The
// subscription auto-unbinds the first time the bound entity is found
// out-of-scope at dispatch time: the wrapped handler reads
// Entity.Zone on every fire and calls UnbindAbility when the entity
// is either destroyed or no longer in any bound zone.
//
// Single-element zones — e.g. []ZoneName{"battlefield"} — model the
// common "while on the battlefield" trigger. Multi-element zones
// model "while in play or graveyard" / "while in any open lane"
// shapes.
//
// Returns an AbilityID so games can also UnbindAbility explicitly
// (counterspell silencing a permanent, scry effect ending a one-shot
// trigger early, etc.).
//
// The library makes one ability-lifetime decision here: "out of named
// zone → unbind." Games that model in-zone sub-states (tap, summoning
// sickness) as Entity.Attrs flags keep their binding through those
// transitions; games that model them as separate zones lose the
// binding. Choose the data model up front; the binding follows.
func (s *State) BindAbility(entity EntityID, zones []ZoneName,
	predicate EventPredicate, handler EventHandler) AbilityID {

	s.nextAbilityID++
	aid := AbilityID(s.nextAbilityID)

	// Defensive copy so callers can't mutate the bound zone list by
	// reusing their slice.
	bound := make([]ZoneName, len(zones))
	copy(bound, zones)

	wrapped := func(state *State, ev Event) {
		e, ok := state.Get(entity)
		if !ok {
			state.UnbindAbility(aid)
			return
		}
		for _, z := range bound {
			if e.Zone == z {
				handler(state, ev)
				return
			}
		}
		state.UnbindAbility(aid)
	}

	subID := s.Subscribe(predicate, wrapped)
	s.abilities = append(s.abilities, ability{
		id: aid, entity: entity, zones: bound, subID: subID,
	})
	return aid
}

// UnbindAbility removes a previously-bound ability. No-op when the id
// is unknown — safe to call from inside a handler that just learned
// its entity left scope, even though that path is already taken care
// of automatically.
func (s *State) UnbindAbility(id AbilityID) {
	for i, a := range s.abilities {
		if a.id == id {
			s.Unsubscribe(a.subID)
			s.abilities = append(s.abilities[:i], s.abilities[i+1:]...)
			return
		}
	}
}
