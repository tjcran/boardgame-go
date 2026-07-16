package ccg

// Trigger staging: the bridge between the event bus and the effect
// queue.
//
// Event handlers run synchronously inside Publish with no access to
// the surrounding move — by design (see event.go). A triggered
// ability that should produce a *pending effect* ("whenever a
// creature enters, draw a card" in a game where draws use the stack)
// therefore should not PushEffect directly from its handler: pushing
// mid-Publish interleaves queue mutations with whatever the
// publishing code is doing, and the push order would depend on
// handler registration order — which is per-process, not part of
// game state, and so would desynchronise replays if it leaked into
// EffectIDs.
//
// Instead, handlers call StageTrigger. Staged effects sit in
// State.StagedTriggers — no ID, no lifecycle event — until the code
// that owns the surrounding move flushes them at a checkpoint of its
// choosing. The classic checkpoints are "after this move's Publish
// calls" and "after each ResolveNext" — i.e. the moments a player
// would receive priority in stack games, or simply "before ending
// the move" in queue games. IDs are minted at flush time, in flush
// order, so replay stability never depends on registration order.
//
// The ordering of a flush is game policy: pass nil to keep staging
// order, or a reorder function — typically built from OrderByPlayer
// — to group simultaneous triggers by controller.
//
// Sketch (stack game, PickBack):
//
//	g.BindAbility(creature, []ZoneName{"battlefield"},
//	    MatchType("creature_entered"),
//	    func(s *State, ev Event) {
//	        s.StageTrigger(EffectFrom(s, creature, "draw", nil))
//	    })
//
//	// in the move, after the Publish that fired the triggers:
//	g.FlushTriggers(func(effs []Effect) []Effect {
//	    return OrderByPlayer(effs, apnapOrder) // [active, non-active]
//	})
//
// With PickBack (stack-top = queue tail) and FlushTriggers pushing in
// slice order, the LAST flushed effect resolves FIRST. So flushing in
// [active player's triggers…, non-active player's triggers…] order
// puts the non-active player's triggers on top of the stack — they
// resolve first, matching the usual "active player's triggers go on
// the stack first" convention.

// StageTrigger appends eff to the staging buffer. No EffectID is
// minted and no lifecycle event is published — both happen at
// FlushTriggers. Safe to call from event handlers, including handlers
// running inside ResolveNext's EventEffectResolving publish.
func (s *State) StageTrigger(eff Effect) {
	eff.ID = 0
	s.StagedTriggers = append(s.StagedTriggers, eff)
}

// EffectFrom builds an Effect stamped with the conventional identity
// of a triggered ability: Source = source, Controller = the source
// entity's EffectiveController (Controller, else Owner). Unknown
// sources yield an empty Controller — the effect is still staged;
// games that care validate at flush.
func EffectFrom(s *State, source EntityID, kind string, data map[string]any) Effect {
	eff := Effect{Source: source, Kind: kind, Data: data}
	if e, ok := s.Get(source); ok {
		eff.Controller = e.EffectiveController()
	}
	return eff
}

// OrderByPlayer stably partitions effs by Controller following
// playerOrder: all of playerOrder[0]'s effects first (keeping their
// relative staging order), then playerOrder[1]'s, and so on. Effects
// whose Controller is absent from playerOrder keep their relative
// staging order after all listed players. Pure function — the input
// slice is not mutated.
//
// This is the generic kernel of APNAP-style ordering: pass the play
// order rotated to the active player and flush the result (see the
// package comment above for how flush order maps to resolution
// order). Within-player ordering choices — games where a player
// orders their own simultaneous triggers — are the caller's: reorder
// each player's run before or instead of this helper.
func OrderByPlayer(effs []Effect, playerOrder []string) []Effect {
	out := make([]Effect, 0, len(effs))
	taken := make([]bool, len(effs))
	for _, p := range playerOrder {
		for i, eff := range effs {
			if !taken[i] && eff.Controller == p {
				out = append(out, eff)
				taken[i] = true
			}
		}
	}
	for i, eff := range effs {
		if !taken[i] {
			out = append(out, eff)
		}
	}
	return out
}

// FlushTriggers drains the staging buffer onto the pending-effect
// queue. reorder (nil = staging order) maps the staged batch to push
// order; each effect is then pushed via PushEffect — minting its ID
// and publishing EventEffectPushed — in slice order. Returns the
// assigned IDs in push order; nil when the buffer was empty.
//
// The buffer is cleared BEFORE pushing, so handlers that fire on
// EventEffectPushed and stage further triggers land in a fresh batch
// for the game's next checkpoint rather than extending this one —
// nested triggers wait their turn, and the flush always terminates.
//
// A reorder function must be deterministic: deriving order from map
// iteration (or anything else non-reproducible) here would replay
// differently than it played. OrderByPlayer and plain slice sorts
// are safe.
func (s *State) FlushTriggers(reorder func([]Effect) []Effect) []EffectID {
	if len(s.StagedTriggers) == 0 {
		return nil
	}
	batch := s.StagedTriggers
	s.StagedTriggers = nil
	if reorder != nil {
		batch = reorder(batch)
	}
	ids := make([]EffectID, 0, len(batch))
	for _, eff := range batch {
		ids = append(ids, s.PushEffect(eff))
	}
	return ids
}
