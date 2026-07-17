package ccg

// Event is one domain notification routed through the bus. Type is the
// arbitrary tag the game defines ("creature_summoned", "damage_dealt",
// "card_drawn"); Source / Target are the entities involved (zero
// values when not applicable); Data is escape-hatch payload.
//
// Events are NOT the same as core.Events (which queues engine
// transitions). These are app-layer signals that the game wants to
// publish/subscribe on top of its core moves.
type Event struct {
	Type   string         `json:"type"`
	Source EntityID       `json:"source,omitempty"`
	Target EntityID       `json:"target,omitempty"`
	Data   map[string]any `json:"data,omitempty"`
}

// EventHandler reacts to an event. Handlers can read/write state,
// publish more events (synchronously fired before Publish returns),
// and call core.MoveContext.Queue.Push to schedule follow-up moves —
// but should be quick: they run inline.
type EventHandler func(state *State, event Event)

// EventPredicate filters which events trigger a subscriber. Return
// true to fire the handler for this event. Predicates should be cheap
// — they run against every published event.
type EventPredicate func(event Event) bool

// SubscriptionID identifies a registered subscriber so the game can
// unsubscribe later (e.g. when a card leaves play and stops listening).
type SubscriptionID uint64

// subscription is the internal record.
type subscription struct {
	id        SubscriptionID
	predicate EventPredicate
	handler   EventHandler
}

// Subscribe registers handler for every event matching predicate.
// Returns a SubscriptionID for later Unsubscribe.
//
// Subscriptions are NOT persisted across serialisation — they live in
// the in-memory bus only. Games re-register subscribers on every
// process start (typically at game-definition time or when permanents
// enter the relevant zone). The persisted state (Modifiers, Events
// history, etc.) survives; the routing table is rebuilt fresh.
func (s *State) Subscribe(predicate EventPredicate, handler EventHandler) SubscriptionID {
	s.IDs.NextSubID++
	id := SubscriptionID(s.IDs.NextSubID)
	s.subs = append(s.subs, subscription{
		id: id, predicate: predicate, handler: handler,
	})
	return id
}

// Unsubscribe removes a previously-registered subscriber. No-op if
// the id is unknown.
func (s *State) Unsubscribe(id SubscriptionID) {
	for i, sub := range s.subs {
		if sub.id == id {
			s.subs = append(s.subs[:i], s.subs[i+1:]...)
			return
		}
	}
}

// Publish routes event to every matching subscriber, in registration
// order. Handlers run synchronously and can call Publish themselves —
// nested events fire depth-first.
//
// Publish appends the event to State.Events (capped at
// MaxEventHistory) so "this turn"-style queries can read recent
// history. Handlers see the freshly-appended event in s.Events.
func (s *State) Publish(event Event) {
	s.Events = append(s.Events, event)
	if len(s.Events) > MaxEventHistory {
		// Drop oldest in chunks of MaxEventHistory/4 so we're not
		// reallocating every Publish once the buffer fills.
		drop := len(s.Events) - MaxEventHistory
		s.Events = s.Events[drop:]
	}
	// Take a snapshot of subscribers so handlers that unsubscribe
	// themselves don't affect this dispatch.
	subs := make([]subscription, len(s.subs))
	copy(subs, s.subs)
	for _, sub := range subs {
		if sub.predicate == nil || sub.predicate(event) {
			sub.handler(s, event)
		}
	}
}

// MaxEventHistory caps how many past events State.Events retains. Apps
// that need full audit logs should mirror their own history; the
// library's buffer is for short-lived "this turn"-style queries.
const MaxEventHistory = 256

// MatchType is a shorthand predicate matching by Event.Type.
func MatchType(t string) EventPredicate {
	return func(e Event) bool { return e.Type == t }
}

// MatchTypeAndSource matches events of the given type from the given
// source entity. Useful for "this specific card's triggers fire."
func MatchTypeAndSource(t string, source EntityID) EventPredicate {
	return func(e Event) bool { return e.Type == t && e.Source == source }
}
