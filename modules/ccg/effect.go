package ccg

// EffectID is the stable handle for a pending effect across its
// lifetime in the queue. IDs are monotonically assigned by
// State.PushEffect / PushEffectFront / InsertEffectAt and survive
// reorderings, mutations, and serialisation until the effect is
// resolved or removed. Replay-safe: equal Setup + equal moves →
// equal IDs.
type EffectID uint64

// Effect is a declarative description of a pending game action — the
// "what should happen" envelope card designers push onto the state
// when they cast a spell, trigger an ability, or resolve an upkeep
// step. The library owns the envelope (struct, queue, dispatch
// helper); the game owns the verbs (Kind values and Resolver
// functions).
//
// Effects sit alongside core.MoveContext.Queue rather than replacing
// it: core.Queue schedules engine moves across the cascade; Effect
// represents one game-level action awaiting resolution inside a
// single move. Move handlers PushEffect, then ResolveNext or hand-roll
// their own loop; subscribers to the lifecycle events
// (EventEffectPushed / EventEffectResolving / …) implement
// counterspells, redirection, replacement, or whatever the game's
// rules need.
//
// Kind is a game-defined verb ("damage", "draw", "summon", …); Data
// is the per-Kind payload (amount, mode, side-chosen target, …). The
// library never inspects Kind or Data — they're contracts between
// the game's card definitions and its resolver table.
type Effect struct {
	ID         EffectID       `json:"id"`
	Source     EntityID       `json:"source,omitempty"`
	Controller string         `json:"controller,omitempty"`
	Kind       string         `json:"kind"`
	Target     EntityID       `json:"target,omitempty"`
	Targets    []EntityID     `json:"targets,omitempty"`
	Data       map[string]any `json:"data,omitempty"`
	Countered  bool           `json:"countered,omitempty"`
	Tag        string         `json:"tag,omitempty"`
}

// Lifecycle event types published on the ccg event bus during effect
// queue mutations and resolution. Subscribers route off these
// constants the same way they route off game-defined Event.Type
// values. Each carries Event.Source = Effect.Source and
// Event.Data["effect_id"] = EffectID so handlers can locate the
// affected entry. EventEffectResolving is the interception point —
// handlers running on that event may MutateEffect, RemoveEffect, or
// set Countered before the resolver fires.
const (
	EventEffectPushed    = "effect_pushed"
	EventEffectResolving = "effect_resolving"
	EventEffectResolved  = "effect_resolved"
	EventEffectRemoved   = "effect_removed"
	EventEffectReplaced  = "effect_replaced"
)

// PushEffect appends eff to the end of the pending queue and
// publishes EventEffectPushed. Returns the assigned EffectID.
//
// "End" means FIFO push / stack bottom — pair with PickFront for
// queue-order resolution. For LIFO / stack-top semantics use
// PushEffectFront.
func (s *State) PushEffect(eff Effect) EffectID {
	s.nextEffectID++
	eff.ID = EffectID(s.nextEffectID)
	s.PendingEffects = append(s.PendingEffects, eff)
	s.publishEffectEvent(EventEffectPushed, eff)
	return eff.ID
}

// PushEffectFront prepends eff to the pending queue and publishes
// EventEffectPushed. Returns the assigned EffectID.
//
// "Front" means LIFO push / stack top — pair with PickFront for
// stack-style "last in, first out" resolution.
func (s *State) PushEffectFront(eff Effect) EffectID {
	s.nextEffectID++
	eff.ID = EffectID(s.nextEffectID)
	s.PendingEffects = append([]Effect{eff}, s.PendingEffects...)
	s.publishEffectEvent(EventEffectPushed, eff)
	return eff.ID
}

// InsertEffectAt inserts eff at index idx (0 == front, len == back)
// and publishes EventEffectPushed. Returns the assigned EffectID.
// Out-of-range idx is clamped to [0, len(PendingEffects)].
//
// Use for interrupt / split-second semantics — slot an effect between
// the currently-resolving entry and the rest of the queue.
func (s *State) InsertEffectAt(idx int, eff Effect) EffectID {
	if idx < 0 {
		idx = 0
	}
	if idx > len(s.PendingEffects) {
		idx = len(s.PendingEffects)
	}
	s.nextEffectID++
	eff.ID = EffectID(s.nextEffectID)
	s.PendingEffects = append(s.PendingEffects, Effect{})
	copy(s.PendingEffects[idx+1:], s.PendingEffects[idx:])
	s.PendingEffects[idx] = eff
	s.publishEffectEvent(EventEffectPushed, eff)
	return eff.ID
}

// FindEffect returns the effect with the given id and its index in
// PendingEffects. The second return is false when no such id exists.
func (s *State) FindEffect(id EffectID) (Effect, int, bool) {
	for i, e := range s.PendingEffects {
		if e.ID == id {
			return e, i, true
		}
	}
	return Effect{}, -1, false
}

// ReplaceEffect swaps the effect with id for next in place, preserving
// the queue index. next.ID is overwritten with id so the handle stays
// stable for subscribers that captured it. Returns true on success,
// false when id is unknown. Publishes EventEffectReplaced.
//
// Use for replacement effects — "if you would draw, draw two instead"
// fires on EventEffectPushed and calls ReplaceEffect with a modified
// payload.
func (s *State) ReplaceEffect(id EffectID, next Effect) bool {
	for i, e := range s.PendingEffects {
		if e.ID == id {
			next.ID = id
			s.PendingEffects[i] = next
			s.publishEffectEvent(EventEffectReplaced, next)
			return true
		}
	}
	return false
}

// RemoveEffect drops the effect with id from the queue and publishes
// EventEffectRemoved. Returns true on success, false when id is
// unknown.
//
// Use for counterspells, fizzles, "exile from stack" — anything that
// pulls a pending effect out of resolution before its resolver runs.
func (s *State) RemoveEffect(id EffectID) bool {
	for i, e := range s.PendingEffects {
		if e.ID == id {
			s.PendingEffects = append(s.PendingEffects[:i], s.PendingEffects[i+1:]...)
			s.publishEffectEvent(EventEffectRemoved, e)
			return true
		}
	}
	return false
}

// MutateEffect applies mutator to a copy of the effect with id and
// writes the result back. The mutator can change any field except ID
// (overwrites are ignored). Returns true on success, false when id is
// unknown.
//
// MutateEffect does NOT publish a lifecycle event — the caller
// already knows what changed and can publish a domain event if
// useful. Use for in-place redirection ("redirect damage to X") and
// payload edits made by EventEffectResolving handlers.
func (s *State) MutateEffect(id EffectID, mutator func(*Effect)) bool {
	for i, e := range s.PendingEffects {
		if e.ID == id {
			mutator(&e)
			e.ID = id
			s.PendingEffects[i] = e
			return true
		}
	}
	return false
}

// Resolver executes one effect against state. Games register one
// Resolver per Kind they define. Resolvers may publish further
// events, push more effects, mutate state — they're just functions.
// Returning a non-nil error aborts ResolveNext and propagates the
// error to its caller; the effect is left on the queue so the caller
// can inspect / retry.
type Resolver func(state *State, eff Effect) error

// ResolverTable maps effect Kind → Resolver. Games typically build
// one table at game-definition time and pass it to ResolveNext.
type ResolverTable map[string]Resolver

// Picker selects which pending effect to resolve next. Returns the
// index into pending, or -1 to halt resolution (queue is exhausted,
// priority window opened, waiting for player input, etc.).
//
// PickFront and PickBack are shipped as helpers; games with priority
// windows, APNAP ordering, or any other custom rule write their own.
type Picker func(pending []Effect) int

// PickFront is the FIFO picker — resolves the head of the queue.
// Returns -1 when empty. Use with PushEffect for queue-order
// resolution (Hearthstone-style triggers).
func PickFront(pending []Effect) int {
	if len(pending) == 0 {
		return -1
	}
	return 0
}

// PickBack is the LIFO / stack picker — resolves the tail of the
// queue. Returns -1 when empty. Use with PushEffect for stack-style
// "last in, first out" resolution (MtG-style stack).
func PickBack(pending []Effect) int {
	return len(pending) - 1
}

// ResolveNext is the optional dispatch helper:
//  1. ask pick which effect to resolve (returning -1 halts);
//  2. publish EventEffectResolving — handlers may MutateEffect,
//     RemoveEffect, or set Countered (the fresh state is re-read
//     after publish so those changes take effect);
//  3. if the effect was removed during step 2, return (zero, false, nil);
//  4. if Countered, publish EventEffectResolved with the countered
//     effect, drop it from the queue, return (effect, true, nil);
//  5. look up table[Kind] — missing entries are an
//     ErrUnknownEffectKind error;
//  6. invoke the resolver; a non-nil error aborts and leaves the
//     effect on the queue;
//  7. publish EventEffectResolved, drop the effect from the queue,
//     return (effect, true, nil).
//
// Games that want full control (e.g. resolve mid-effect without
// removing, or batch multiple effects atomically) can ignore this
// helper and iterate PendingEffects directly.
func (s *State) ResolveNext(pick Picker, table ResolverTable) (Effect, bool, error) {
	idx := pick(s.PendingEffects)
	if idx < 0 || idx >= len(s.PendingEffects) {
		return Effect{}, false, nil
	}
	eff := s.PendingEffects[idx]
	s.publishEffectEvent(EventEffectResolving, eff)

	// Re-read: handlers may have removed or mutated the effect.
	current, curIdx, ok := s.FindEffect(eff.ID)
	if !ok {
		return Effect{}, false, nil
	}
	if current.Countered {
		s.PendingEffects = append(s.PendingEffects[:curIdx], s.PendingEffects[curIdx+1:]...)
		s.publishEffectEvent(EventEffectResolved, current)
		return current, true, nil
	}
	resolver, ok := table[current.Kind]
	if !ok {
		return current, false, &UnknownEffectKindError{Kind: current.Kind}
	}
	if err := resolver(s, current); err != nil {
		return current, false, err
	}
	// Re-locate: resolver may have pushed/removed siblings, shifting
	// indices. Look up by ID, not the cached index.
	if _, finalIdx, ok := s.FindEffect(current.ID); ok {
		s.PendingEffects = append(s.PendingEffects[:finalIdx], s.PendingEffects[finalIdx+1:]...)
	}
	s.publishEffectEvent(EventEffectResolved, current)
	return current, true, nil
}

// UnknownEffectKindError is returned by ResolveNext when an effect's
// Kind has no entry in the ResolverTable. Games typically treat this
// as a programming error (a card pushed an effect the game never
// registered a verb for).
type UnknownEffectKindError struct {
	Kind string
}

func (e *UnknownEffectKindError) Error() string {
	return "ccg: unknown effect kind " + e.Kind
}

// publishEffectEvent emits a lifecycle event tagged with the effect's
// ID and Source. Internal helper; uses Publish so handlers run
// synchronously and depth-first, matching the rest of the bus.
func (s *State) publishEffectEvent(eventType string, eff Effect) {
	s.Publish(Event{
		Type:   eventType,
		Source: eff.Source,
		Target: eff.Target,
		Data:   map[string]any{"effect_id": eff.ID},
	})
}
