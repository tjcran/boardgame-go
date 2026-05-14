package ccg

import "github.com/tjcran/boardgame-go/core"

// DeckPile labels a (deck, discard) zone pair and provides the
// composite operations every CCG re-implements: drawing with optional
// auto-reshuffle when the deck runs dry, discarding a batch, milling
// the top N cards, and shuffling the discard pile back into the deck.
//
// DeckPile does not own its zones — it is a *label* that names two
// ZoneNames already (or about-to-be) registered in the State. Games
// can mix direct zone calls (state.Draw(deckName, n)) with pile
// methods freely.
//
// The pile struct itself is value-typed and JSON-friendly. Games
// typically embed a *DeckPile in their G or build one fresh each
// move from known zone names — both are fine.
type DeckPile struct {
	// DeckZone is the ZoneName for the draw pile.
	DeckZone ZoneName `json:"deck"`
	// DiscardZone is the ZoneName for the discard pile.
	DiscardZone ZoneName `json:"discard"`

	// AutoReshuffleOnEmpty, when true, makes Draw and Mill shuffle the
	// Discard back into Deck (using the passed *core.Random) whenever
	// they start a call against an empty deck. Default false — MTG /
	// Pokemon style, where an empty library has a game-specific
	// consequence (fatigue, deck-out loss) that the library should not
	// auto-resolve. Hearthstone-style "deck wraps around" games flip
	// this to true at construction.
	AutoReshuffleOnEmpty bool `json:"auto_reshuffle_on_empty,omitempty"`
}

// NewDeckPile registers (or finds, if already declared) the deck and
// discard zones — both ordered, so position-on-top-of-pile is stable
// and shuffles are well-defined — and returns a DeckPile labelling
// them. Idempotent: re-declaring with a different `ordered` flag is a
// no-op because NewZone is idempotent.
func (s *State) NewDeckPile(deck, discard ZoneName) *DeckPile {
	s.NewZone(deck, true)
	s.NewZone(discard, true)
	return &DeckPile{DeckZone: deck, DiscardZone: discard}
}

// Draw pops the top n entities off the deck and returns them in
// draw order. When AutoReshuffleOnEmpty is set and the deck is empty
// at the start of the call, the discard is first shuffled back into
// the deck via the passed *core.Random; the reshuffle happens once
// per call, not per drawn card. Returns ErrZoneEmpty when both deck
// and discard are empty (or the deck is empty and auto-reshuffle is
// off).
//
// The drawn entities have Entity.Zone cleared — callers typically
// follow up by Transition(drawn, handZone). Use Mill if the
// destination is the discard.
func (p *DeckPile) Draw(s *State, n int, r *core.Random) ([]EntityID, error) {
	if s.Size(p.DeckZone) == 0 && p.AutoReshuffleOnEmpty && s.Size(p.DiscardZone) > 0 {
		if err := p.ReshuffleDiscardIntoDeck(s, r); err != nil {
			return nil, err
		}
	}
	return s.Draw(p.DeckZone, n)
}

// Discard moves the given entity ids into the discard zone with the
// same pre-flight all-or-nothing semantics as Transition: if any id
// is unknown or the discard is at capacity, state stays untouched and
// the error is returned.
func (p *DeckPile) Discard(s *State, ids ...EntityID) error {
	return s.Transition(ids, p.DiscardZone)
}

// Mill draws the top n entities off the deck and moves them directly
// to the discard zone, returning the milled ids in draw order. Same
// AutoReshuffleOnEmpty behaviour as Draw. If Draw succeeds but the
// follow-up Discard fails (e.g. discard at capacity), the drawn
// entities are returned alongside the error so the caller can
// recover — they currently sit zone-less (Draw cleared Entity.Zone)
// and require an explicit MoveTo to land somewhere.
func (p *DeckPile) Mill(s *State, n int, r *core.Random) ([]EntityID, error) {
	drawn, err := p.Draw(s, n, r)
	if err != nil {
		return nil, err
	}
	if err := s.Transition(drawn, p.DiscardZone); err != nil {
		return drawn, err
	}
	return drawn, nil
}

// ReshuffleDiscardIntoDeck moves every entity from the discard into
// the deck and shuffles the deck via the engine PRNG. Used directly
// for explicit "shuffle your graveyard into your library" effects;
// also called internally by Draw / Mill when AutoReshuffleOnEmpty is
// set.
//
// No-op (returns nil) when the discard is empty.
func (p *DeckPile) ReshuffleDiscardIntoDeck(s *State, r *core.Random) error {
	if s.Size(p.DiscardZone) == 0 {
		return nil
	}
	if err := s.MoveAllTo(p.DeckZone, p.DiscardZone); err != nil {
		return err
	}
	return s.Shuffle(p.DeckZone, r)
}

// DeckSize / DiscardSize report current counts. Convenience for "is
// the deck about to run out?" style guards.
func (p *DeckPile) DeckSize(s *State) int    { return s.Size(p.DeckZone) }
func (p *DeckPile) DiscardSize(s *State) int { return s.Size(p.DiscardZone) }
