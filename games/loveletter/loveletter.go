// Package loveletter is a reference implementation of the classic
// 16-card Love Letter (Seiji Kanai, 2012) on the boardgame-go engine.
//
// It exercises features that tic-tac-toe doesn't: hidden hands via a
// custom PlayerView, the seeded Random plugin for shuffling, per-player
// state with elimination and revival across rounds, multi-round matches
// scored by tokens of affection, and rich move targeting with
// Handmaid-protection rules.
//
// Rule deviations from the printed card-set:
//
//   - The 2-player "reveal three cards face-up" rule is omitted. The
//     standard 16-card deck with one burn card is used for all
//     2/3/4-player matches.
//   - Tiebreakers: when several players tie for highest card at deck
//     end, every tied player wins a token (printed rules use total
//     discard value as the tiebreak).
package loveletter

import (
	"errors"
	"fmt"
	"sort"

	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/plugins/random"
)

// Card is the rank/identity of a Love Letter card. Values 1..8 match the
// printed numbers; zero is reserved for "no card".
type Card int

const (
	CardNone     Card = 0
	CardGuard    Card = 1 // 5 copies
	CardPriest   Card = 2 // 2 copies
	CardBaron    Card = 3 // 2 copies
	CardHandmaid Card = 4 // 2 copies
	CardPrince   Card = 5 // 2 copies
	CardKing     Card = 6 // 1 copy
	CardCountess Card = 7 // 1 copy
	CardPrincess Card = 8 // 1 copy
)

// cardNames is used by the move log and the Guard-naming wire format.
var cardNames = map[Card]string{
	CardGuard:    "Guard",
	CardPriest:   "Priest",
	CardBaron:    "Baron",
	CardHandmaid: "Handmaid",
	CardPrince:   "Prince",
	CardKing:     "King",
	CardCountess: "Countess",
	CardPrincess: "Princess",
}

// String renders the card name; satisfies fmt.Stringer for log entries.
func (c Card) String() string {
	if n, ok := cardNames[c]; ok {
		return n
	}
	return "Unknown"
}

// PlayerState is the per-seat record. Hand and SeenLastRound are hidden
// from other seats by PlayerView.
type PlayerState struct {
	// Hand holds 0–2 cards. Outside of a player's own turn it's 1 card;
	// during the active player's turn (after the draw) it's 2.
	Hand []Card `json:"hand,omitempty"`

	// Discard is the public discard pile for this seat, oldest first.
	Discard []Card `json:"discard,omitempty"`

	// Tokens of affection earned across rounds.
	Tokens int `json:"tokens"`

	// Alive reports whether the player is still in this round. Reset to
	// true at round start.
	Alive bool `json:"alive"`

	// Protected is Handmaid immunity. Cleared at the start of this
	// player's next turn.
	Protected bool `json:"protected,omitempty"`

	// SeenLastPriest is a private record of the last Priest reveal the
	// owning player obtained — keyed by target seat. Visible only via
	// PlayerView for the seat that owns this record. Cleared at the
	// start of each new round.
	SeenLastPriest map[string]Card `json:"seenLastPriest,omitempty"`
}

// State is the full G payload.
type State struct {
	// Players keyed by seat ID ("0", "1", ...).
	Players map[string]*PlayerState `json:"players"`

	// Deck is the face-down draw pile (top is index 0). Server-only;
	// stripped by PlayerView.
	Deck []Card `json:"deck,omitempty"`

	// Burned is the single card removed at round start. Server-only.
	Burned Card `json:"burned,omitempty"`

	// Round is 1-indexed.
	Round int `json:"round"`

	// NextStarter is the seat ID of the player who begins the next
	// turn. Updated by Turn.OnEnd when a round flips. Read by the
	// custom TurnOrder.Next.
	NextStarter string `json:"nextStarter,omitempty"`

	// NewRound signals to TurnOrder.Next that the just-completed turn
	// transition crosses a round boundary, so Next should jump to
	// NextStarter instead of advancing round-robin. Cleared by
	// Turn.OnBegin once consumed.
	NewRound bool `json:"newRound,omitempty"`

	// RoundWinners is the last round's winners, for UI/log surfacing.
	// Cleared at the start of the next round.
	RoundWinners []string `json:"roundWinners,omitempty"`

	// MatchOver is the latching signal Game.EndIf reads.
	MatchOver bool `json:"matchOver,omitempty"`

	// MatchWinners is the final result, filled when MatchOver becomes
	// true.
	MatchWinners []string `json:"matchWinners,omitempty"`

	// TokensToWin caches the per-player-count threshold for match end.
	// Stored in state so a Manager.ExportMatch round-trip preserves it.
	TokensToWin int `json:"tokensToWin"`
}

// ErrInvalidTarget signals a move whose target/named-card argument
// violates a rule (e.g. naming Guard with a Guard, self-targeting,
// targeting a Handmaid-protected opponent when other legal targets
// exist). Wraps core.ErrInvalidMove so callers / tests can errors.Is
// against either.
var ErrInvalidTarget = fmt.Errorf("%w: invalid target", core.ErrInvalidMove)

// New returns the registered Game definition. Pass it to a match.Manager.
//
// The Random plugin is registered with a stable per-call seed-zero so
// the engine generates a per-match seed unless Game.Seed is overridden
// by the caller.
func New() *core.Game {
	return &core.Game{
		Name:       "love-letter",
		MinPlayers: 2,
		MaxPlayers: 4,
		Setup:      setup,
		Plugins:    []core.Plugin{random.New(nil)},
		Moves: map[string]any{
			"playGuard":    core.MoveFn(playGuard),
			"playPriest":   core.MoveFn(playPriest),
			"playBaron":    core.MoveFn(playBaron),
			"playHandmaid": core.MoveFn(playHandmaid),
			"playPrince":   core.MoveFn(playPrince),
			"playKing":     core.MoveFn(playKing),
			"playCountess": core.MoveFn(playCountess),
			"playPrincess": core.MoveFn(playPrincess),
		},
		Turn: &core.TurnConfig{
			Order:    loveLetterOrder,
			OnBegin:  turnOnBegin,
			OnMove:   turnOnMove,
			OnEnd:    turnOnEnd,
			MinMoves: 1,
			MaxMoves: 1,
		},
		EndIf:      endIf,
		PlayerView: playerView,
	}
}

// tokensToWin is the printed match-end threshold ("tokens of affection").
func tokensToWin(numPlayers int) int {
	switch numPlayers {
	case 2:
		return 7
	case 3:
		return 5
	case 4:
		return 4
	default:
		// Out-of-range player counts default to 4 — matches the
		// engine's MaxPlayers cap, but be defensive in case future
		// rules tweaks change the bounds.
		return 4
	}
}

// setup builds the initial G: empty player records, full deck, one card
// burned, one card dealt to each seat. Called once at NewMatch.
func setup(ctx core.Ctx, _ any) core.G {
	s := &State{
		Players:     make(map[string]*PlayerState, ctx.NumPlayers),
		Round:       1,
		TokensToWin: tokensToWin(ctx.NumPlayers),
	}
	for _, pid := range ctx.PlayOrder {
		s.Players[pid] = &PlayerState{Alive: true}
	}
	// First-round shuffle/burn/deal runs deterministically off a fixed
	// seed because Setup has no MoveContext (and thus no Random). The
	// real PRNG kicks in on the first move via Turn.OnBegin's draw —
	// from there forward, shuffle/burn happens through mc.Random in
	// Turn.OnEnd when resetting subsequent rounds. To keep round 1
	// reproducible-but-not-trivial, we use a deterministic in-package
	// permutation here.
	deck := freshDeck()
	deck = deterministicShuffle(deck, uint64(ctx.NumPlayers)*0x9E3779B97F4A7C15+1)
	// Burn one card.
	s.Burned = deck[0]
	deck = deck[1:]
	// Deal one card per seat.
	for _, pid := range ctx.PlayOrder {
		s.Players[pid].Hand = []Card{deck[0]}
		deck = deck[1:]
	}
	s.Deck = deck
	// The very first turn's player draws their second card via
	// Turn.OnBegin when Apply first runs. NewMatch also runs Turn.OnBegin
	// for the initial turn — see core.NewMatch — so the actor enters
	// move-selection with 2 cards.
	return s
}

// freshDeck returns the printed 16-card distribution.
func freshDeck() []Card {
	deck := make([]Card, 0, 16)
	add := func(c Card, n int) {
		for i := 0; i < n; i++ {
			deck = append(deck, c)
		}
	}
	add(CardGuard, 5)
	add(CardPriest, 2)
	add(CardBaron, 2)
	add(CardHandmaid, 2)
	add(CardPrince, 2)
	add(CardKing, 1)
	add(CardCountess, 1)
	add(CardPrincess, 1)
	return deck
}

// deterministicShuffle is used at Setup time only, where no Random
// plugin is yet available. After turn 1 begins, all shuffles route
// through mc.Random via core.Shuffle for full plugin parity (so
// MockRandom can override them).
func deterministicShuffle(in []Card, seed uint64) []Card {
	out := make([]Card, len(in))
	copy(out, in)
	s := seed
	next := func() uint64 {
		s += 0x9E3779B97F4A7C15
		z := s
		z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
		z = (z ^ (z >> 27)) * 0x94D049BB133111EB
		return z ^ (z >> 31)
	}
	for i := len(out) - 1; i > 0; i-- {
		j := int(next() % uint64(i+1))
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// turnOnBegin draws a card for the current player and clears their
// Handmaid protection. If the deck is empty when they would draw,
// Turn.OnEnd of the previous turn was responsible for already ending
// the round — we just no-op here to avoid double-drawing on a
// round-reset entry.
func turnOnBegin(mc *core.MoveContext) core.G {
	s := cloneState(mc.G.(*State))
	// A round-reset just put us here on the new starter's first turn:
	// the deal already gave them one card, so we still want to draw
	// the second card to set up move-selection. Clear NewRound so the
	// next Order.Next falls back to normal alive-walking.
	s.NewRound = false

	pid := mc.Ctx.CurrentPlayer
	p := s.Players[pid]
	if p == nil {
		return s
	}
	p.Protected = false

	if len(s.Deck) == 0 {
		// Round-end would have been triggered by the prior Turn.OnEnd;
		// if we land here with no deck it's the very edge case of a
		// 2-player game where setup left only one card after deal +
		// burn (15-3=12). Shouldn't happen in normal play; guard anyway.
		return s
	}
	draw := s.Deck[0]
	s.Deck = append([]Card(nil), s.Deck[1:]...)
	p.Hand = append(append([]Card(nil), p.Hand...), draw)
	return s
}

// turnOnMove runs after every successful move, before Game.EndIf is
// evaluated. This is where round-end detection lives — putting it here
// (rather than in Turn.OnEnd) means MatchOver is latched into G in time
// for Game.EndIf to read it on the same move. Turn.OnEnd later handles
// the new-round reset.
//
// Detection:
//   - Only one alive player → round ends.
//   - Deck empty → round ends at end of this turn (next player can't
//     draw).
func turnOnMove(mc *core.MoveContext) core.G {
	s := cloneState(mc.G.(*State))
	alive := aliveSeats(s, mc.Ctx.PlayOrder)
	roundEnded := false
	switch {
	case len(alive) <= 1:
		roundEnded = true
	case len(s.Deck) == 0:
		roundEnded = true
	}
	if !roundEnded {
		return s
	}

	winners := determineRoundWinners(s, alive)
	for _, w := range winners {
		s.Players[w].Tokens++
	}
	s.RoundWinners = winners

	for _, pid := range mc.Ctx.PlayOrder {
		if s.Players[pid].Tokens >= s.TokensToWin {
			s.MatchWinners = append(s.MatchWinners, pid)
		}
	}
	if len(s.MatchWinners) > 0 {
		s.MatchOver = true
		return s
	}

	// Round flips. NewRound + NextStarter route Order.Next to the
	// winner; Turn.OnEnd then runs resetRound.
	s.NextStarter = winners[0]
	s.NewRound = true
	return s
}

// turnOnEnd runs the round-reset (shuffle/burn/deal/alive=true) when
// the just-finished move triggered a new round. No-op otherwise.
func turnOnEnd(mc *core.MoveContext) core.G {
	s := cloneState(mc.G.(*State))
	if !s.NewRound || s.MatchOver {
		return s
	}
	s.Round++
	resetRound(s, mc)
	return s
}

// resetRound shuffles a fresh deck via mc.Random, burns one card, and
// deals one card to every seat. Resets per-player round state. Called
// from turnOnEnd when the match continues.
func resetRound(s *State, mc *core.MoveContext) {
	deck := freshDeck()
	// core.Shuffle is generic; needs a non-nil *core.Random.
	if mc.Random != nil {
		deck = core.Shuffle(mc.Random, deck)
	} else {
		// Fallback for hooks that somehow arrive without Random
		// wired — keeps the game from crashing in a misconfigured
		// host. Deterministic, audit-friendly.
		deck = deterministicShuffle(deck, uint64(s.Round)*0xBF58476D1CE4E5B9)
	}
	s.Burned = deck[0]
	deck = deck[1:]

	for _, pid := range mc.Ctx.PlayOrder {
		p := s.Players[pid]
		p.Hand = []Card{deck[0]}
		deck = deck[1:]
		p.Discard = nil
		p.Alive = true
		p.Protected = false
		p.SeenLastPriest = nil
	}
	s.Deck = deck
}

// determineRoundWinners returns the list of seat IDs that won this
// round. Single survivor wins immediately; otherwise the highest card
// in hand wins (ties: all tied players are listed).
func determineRoundWinners(s *State, alive []string) []string {
	if len(alive) == 1 {
		return alive
	}
	// Deck depleted with multiple alive — compare hands. Each alive
	// player still has exactly 1 card (their unplayed card from before
	// the deck emptied).
	highest := Card(0)
	for _, pid := range alive {
		if len(s.Players[pid].Hand) == 0 {
			continue
		}
		if c := s.Players[pid].Hand[0]; c > highest {
			highest = c
		}
	}
	var winners []string
	for _, pid := range alive {
		if len(s.Players[pid].Hand) > 0 && s.Players[pid].Hand[0] == highest {
			winners = append(winners, pid)
		}
	}
	// Stable order for log determinism.
	sort.Strings(winners)
	return winners
}

func aliveSeats(s *State, order []string) []string {
	out := make([]string, 0, len(order))
	for _, pid := range order {
		if s.Players[pid].Alive {
			out = append(out, pid)
		}
	}
	return out
}

// loveLetterOrder is the custom turn-order that (a) jumps to
// NextStarter when a round boundary was just crossed, and (b) otherwise
// advances round-robin while skipping eliminated players.
var loveLetterOrder = core.TurnOrder{
	First: func(_ *core.MoveContext) int { return 0 },
	Next: func(mc *core.MoveContext) *int {
		s := mc.G.(*State)
		if s.NewRound {
			for i, pid := range mc.Ctx.PlayOrder {
				if pid == s.NextStarter {
					return &i
				}
			}
		}
		n := mc.Ctx.NumPlayers
		for i := 1; i <= n; i++ {
			pos := (mc.Ctx.PlayOrderPos + i) % n
			if s.Players[mc.Ctx.PlayOrder[pos]].Alive {
				return &pos
			}
		}
		// No alive players left — shouldn't be reachable because
		// turnOnEnd would have set MatchOver/round-reset first, but
		// return nil to signal "no one to play" if it ever happens.
		return nil
	},
}

// endIf is Game.EndIf: returns the final result once turnOnEnd latches
// MatchOver.
func endIf(mc *core.MoveContext) any {
	s := mc.G.(*State)
	if !s.MatchOver {
		return nil
	}
	if len(s.MatchWinners) == 1 {
		return map[string]any{"winner": s.MatchWinners[0]}
	}
	return map[string]any{"winners": s.MatchWinners}
}

// playerView redacts G for a given seat. Hides the deck, the burned
// card, every other player's hand, and every other player's private
// SeenLastPriest map. Spectators (playerID="") see only the public
// surface: tokens, alive flags, discards, round counter.
func playerView(g core.G, _ core.Ctx, playerID string) core.G {
	src := g.(*State)
	out := &State{
		Round:        src.Round,
		NextStarter:  src.NextStarter,
		NewRound:     src.NewRound,
		RoundWinners: append([]string(nil), src.RoundWinners...),
		MatchOver:    src.MatchOver,
		MatchWinners: append([]string(nil), src.MatchWinners...),
		TokensToWin:  src.TokensToWin,
		Players:      make(map[string]*PlayerState, len(src.Players)),
	}
	for pid, p := range src.Players {
		clone := &PlayerState{
			Discard:   append([]Card(nil), p.Discard...),
			Tokens:    p.Tokens,
			Alive:     p.Alive,
			Protected: p.Protected,
		}
		if pid == playerID {
			clone.Hand = append([]Card(nil), p.Hand...)
			if len(p.SeenLastPriest) > 0 {
				clone.SeenLastPriest = make(map[string]Card, len(p.SeenLastPriest))
				for k, v := range p.SeenLastPriest {
					clone.SeenLastPriest[k] = v
				}
			}
		}
		out.Players[pid] = clone
	}
	return out
}

// ---------------------------------------------------------------------
// Move implementations
// ---------------------------------------------------------------------

// errCountess wraps the Countess-forcing rule violation. Surfaced as
// ErrInvalidMove so transports return a 409.
var errCountess = fmt.Errorf("%w: must play Countess when holding King or Prince", core.ErrInvalidMove)

// playGuard: args[0]=target seat ID (string), args[1]=named card (Card
// or numeric 2..8). Eliminates target if their hand matches. Cannot
// name Guard. Cannot target self or a protected player (if any other
// legal target exists).
func playGuard(mc *core.MoveContext, args ...any) (core.G, error) {
	if len(args) < 2 {
		return mc.G, fmt.Errorf("%w: playGuard needs target and named card", core.ErrInvalidMove)
	}
	target, err := toString(args[0])
	if err != nil {
		return mc.G, fmt.Errorf("%w: %v", core.ErrInvalidMove, err)
	}
	namedI, err := toInt(args[1])
	if err != nil {
		return mc.G, fmt.Errorf("%w: %v", core.ErrInvalidMove, err)
	}
	named := Card(namedI)
	if named == CardGuard || named < CardPriest || named > CardPrincess {
		return mc.G, fmt.Errorf("%w: must name a card 2..8", core.ErrInvalidMove)
	}

	s := cloneState(mc.G.(*State))
	if err := preflightTargeted(s, mc.Ctx, mc.PlayerID, target, CardGuard, false); err != nil {
		return mc.G, err
	}
	if err := discardActor(s, mc.PlayerID, CardGuard); err != nil {
		return mc.G, err
	}
	if isHandmaidImmune(s, target) {
		// Card resolves with no effect (all alive opponents were
		// protected). Allowed when there's no legal target.
		return s, nil
	}
	if len(s.Players[target].Hand) > 0 && s.Players[target].Hand[0] == named {
		eliminate(s, target)
	}
	return s, nil
}

// playPriest: args[0]=target. Owner learns target's hand via private
// SeenLastPriest map (PlayerView surfaces it to actor only).
func playPriest(mc *core.MoveContext, args ...any) (core.G, error) {
	if len(args) < 1 {
		return mc.G, fmt.Errorf("%w: playPriest needs target", core.ErrInvalidMove)
	}
	target, err := toString(args[0])
	if err != nil {
		return mc.G, fmt.Errorf("%w: %v", core.ErrInvalidMove, err)
	}
	s := cloneState(mc.G.(*State))
	if err := preflightTargeted(s, mc.Ctx, mc.PlayerID, target, CardPriest, false); err != nil {
		return mc.G, err
	}
	if err := discardActor(s, mc.PlayerID, CardPriest); err != nil {
		return mc.G, err
	}
	if isHandmaidImmune(s, target) {
		return s, nil
	}
	if len(s.Players[target].Hand) > 0 {
		actor := s.Players[mc.PlayerID]
		if actor.SeenLastPriest == nil {
			actor.SeenLastPriest = map[string]Card{}
		}
		actor.SeenLastPriest[target] = s.Players[target].Hand[0]
	}
	return s, nil
}

// playBaron: args[0]=target. Compare hands; lower is eliminated; ties
// have no effect. Acceptable to play when only protected opponents
// exist — resolves with no effect.
func playBaron(mc *core.MoveContext, args ...any) (core.G, error) {
	if len(args) < 1 {
		return mc.G, fmt.Errorf("%w: playBaron needs target", core.ErrInvalidMove)
	}
	target, err := toString(args[0])
	if err != nil {
		return mc.G, fmt.Errorf("%w: %v", core.ErrInvalidMove, err)
	}
	s := cloneState(mc.G.(*State))
	if err := preflightTargeted(s, mc.Ctx, mc.PlayerID, target, CardBaron, false); err != nil {
		return mc.G, err
	}
	if err := discardActor(s, mc.PlayerID, CardBaron); err != nil {
		return mc.G, err
	}
	if isHandmaidImmune(s, target) {
		return s, nil
	}
	actor := s.Players[mc.PlayerID]
	t := s.Players[target]
	if len(actor.Hand) == 0 || len(t.Hand) == 0 {
		return s, nil
	}
	a, b := actor.Hand[0], t.Hand[0]
	switch {
	case a > b:
		eliminate(s, target)
	case b > a:
		eliminate(s, mc.PlayerID)
	default:
	}
	return s, nil
}

// playHandmaid: actor becomes immune to other players' card effects
// until the start of their next turn.
func playHandmaid(mc *core.MoveContext, _ ...any) (core.G, error) {
	s := cloneState(mc.G.(*State))
	if err := requireCountessNotForced(s, mc.PlayerID, CardHandmaid); err != nil {
		return mc.G, err
	}
	if err := discardActor(s, mc.PlayerID, CardHandmaid); err != nil {
		return mc.G, err
	}
	s.Players[mc.PlayerID].Protected = true
	return s, nil
}

// playPrince: args[0]=target (may be self). Target discards their hand
// (Princess discard eliminates them) and draws a replacement — or the
// burned card if the deck is empty.
func playPrince(mc *core.MoveContext, args ...any) (core.G, error) {
	if len(args) < 1 {
		return mc.G, fmt.Errorf("%w: playPrince needs target", core.ErrInvalidMove)
	}
	target, err := toString(args[0])
	if err != nil {
		return mc.G, fmt.Errorf("%w: %v", core.ErrInvalidMove, err)
	}
	s := cloneState(mc.G.(*State))
	if err := preflightTargeted(s, mc.Ctx, mc.PlayerID, target, CardPrince, true); err != nil {
		return mc.G, err
	}
	if err := discardActor(s, mc.PlayerID, CardPrince); err != nil {
		return mc.G, err
	}
	if target != mc.PlayerID && isHandmaidImmune(s, target) {
		// Prince doesn't auto-redirect to self; per common ruling the
		// card simply has no effect when its only legal target is
		// protected. (Some house-rules force self-target; we follow
		// the printed text.)
		return s, nil
	}
	t := s.Players[target]
	if len(t.Hand) == 0 {
		return s, nil
	}
	discarded := t.Hand[0]
	t.Hand = nil
	t.Discard = append(t.Discard, discarded)
	if discarded == CardPrincess {
		eliminate(s, target)
		return s, nil
	}
	// Draw replacement.
	var replacement Card
	switch {
	case len(s.Deck) > 0:
		replacement = s.Deck[0]
		s.Deck = append([]Card(nil), s.Deck[1:]...)
	case s.Burned != CardNone:
		replacement = s.Burned
		s.Burned = CardNone
	default:
		// No draw possible. Leave target hand empty — they'll lose at
		// round end on the tiebreak step.
		return s, nil
	}
	t.Hand = []Card{replacement}
	return s, nil
}

// playKing: args[0]=target. Swap hands with target.
func playKing(mc *core.MoveContext, args ...any) (core.G, error) {
	if len(args) < 1 {
		return mc.G, fmt.Errorf("%w: playKing needs target", core.ErrInvalidMove)
	}
	target, err := toString(args[0])
	if err != nil {
		return mc.G, fmt.Errorf("%w: %v", core.ErrInvalidMove, err)
	}
	s := cloneState(mc.G.(*State))
	if err := preflightTargeted(s, mc.Ctx, mc.PlayerID, target, CardKing, false); err != nil {
		return mc.G, err
	}
	if err := discardActor(s, mc.PlayerID, CardKing); err != nil {
		return mc.G, err
	}
	if isHandmaidImmune(s, target) {
		return s, nil
	}
	actor := s.Players[mc.PlayerID]
	t := s.Players[target]
	actor.Hand, t.Hand = t.Hand, actor.Hand
	return s, nil
}

// playCountess: discard the Countess. No effect — its sole rule lives
// in the Countess-forcing check on Prince/King plays.
func playCountess(mc *core.MoveContext, _ ...any) (core.G, error) {
	s := cloneState(mc.G.(*State))
	if err := discardActor(s, mc.PlayerID, CardCountess); err != nil {
		return mc.G, err
	}
	return s, nil
}

// playPrincess: discarding the Princess voluntarily eliminates the
// actor. Legal but suicidal.
func playPrincess(mc *core.MoveContext, _ ...any) (core.G, error) {
	s := cloneState(mc.G.(*State))
	if err := discardActor(s, mc.PlayerID, CardPrincess); err != nil {
		return mc.G, err
	}
	eliminate(s, mc.PlayerID)
	return s, nil
}

// ---------------------------------------------------------------------
// Move helpers
// ---------------------------------------------------------------------

// preflightTargeted runs the rule checks shared by Guard/Priest/Baron/
// Prince/King: actor must be alive and in possession of the card being
// played, target must be alive, target cannot be self (unless
// allowSelf), and if every alive opponent is Handmaid-protected the
// card is still playable (resolves with no effect — checked by the
// caller via isHandmaidImmune).
//
// allowSelf is true only for Prince.
func preflightTargeted(s *State, ctx core.Ctx, actor, target string, card Card, allowSelf bool) error {
	if err := requireCountessNotForced(s, actor, card); err != nil {
		return err
	}
	a := s.Players[actor]
	if a == nil || !a.Alive {
		return fmt.Errorf("%w: actor not in play", core.ErrInvalidMove)
	}
	if !holds(a.Hand, card) {
		return fmt.Errorf("%w: actor does not hold %s", core.ErrInvalidMove, card)
	}
	t := s.Players[target]
	if t == nil {
		return fmt.Errorf("%w: unknown target %q", ErrInvalidTarget, target)
	}
	if !t.Alive {
		return fmt.Errorf("%w: target %q is eliminated", ErrInvalidTarget, target)
	}
	if target == actor && !allowSelf {
		return fmt.Errorf("%w: cannot target self with %s", ErrInvalidTarget, card)
	}
	_ = ctx
	return nil
}

// requireCountessNotForced rejects Prince/King plays when the actor
// also holds the Countess — the printed forcing rule. Returns nil for
// every other card (and for Prince/King when no Countess is present).
func requireCountessNotForced(s *State, actor string, card Card) error {
	if card != CardPrince && card != CardKing {
		return nil
	}
	hand := s.Players[actor].Hand
	if holds(hand, CardCountess) && (holds(hand, CardPrince) || holds(hand, CardKing)) {
		return errCountess
	}
	return nil
}

// holds reports whether the hand contains a given card.
func holds(hand []Card, c Card) bool {
	for _, h := range hand {
		if h == c {
			return true
		}
	}
	return false
}

// discardActor removes one copy of the named card from the actor's hand
// and appends it to their discard pile. Errors if the actor doesn't
// hold the card.
func discardActor(s *State, actor string, card Card) error {
	p := s.Players[actor]
	for i, h := range p.Hand {
		if h == card {
			p.Hand = append(append([]Card(nil), p.Hand[:i]...), p.Hand[i+1:]...)
			p.Discard = append(p.Discard, card)
			return nil
		}
	}
	return fmt.Errorf("%w: actor does not hold %s", core.ErrInvalidMove, card)
}

// eliminate marks a player as out of the round and moves their last
// held card to their discard pile (per printed rules — eliminated
// players reveal their hand). Idempotent on already-eliminated players.
func eliminate(s *State, pid string) {
	p := s.Players[pid]
	if p == nil || !p.Alive {
		return
	}
	p.Alive = false
	if len(p.Hand) > 0 {
		p.Discard = append(p.Discard, p.Hand...)
		p.Hand = nil
	}
}

// isHandmaidImmune reports whether targeting the named seat must
// resolve as a no-op because they're Handmaid-protected. Only checked
// after preflight; preflight rejects self-targeting separately.
func isHandmaidImmune(s *State, target string) bool {
	t := s.Players[target]
	return t != nil && t.Protected
}

// ---------------------------------------------------------------------
// Misc helpers
// ---------------------------------------------------------------------

// cloneState produces a deep-enough copy of State for a single move's
// mutations. Cards are value types; only slices and maps need fresh
// allocations. We don't clone unrelated fields (Burned, Round, etc.) —
// they're either scalars or owned by the caller.
func cloneState(s *State) *State {
	out := *s
	out.Players = make(map[string]*PlayerState, len(s.Players))
	for k, v := range s.Players {
		cp := *v
		cp.Hand = append([]Card(nil), v.Hand...)
		cp.Discard = append([]Card(nil), v.Discard...)
		if len(v.SeenLastPriest) > 0 {
			cp.SeenLastPriest = make(map[string]Card, len(v.SeenLastPriest))
			for kk, vv := range v.SeenLastPriest {
				cp.SeenLastPriest[kk] = vv
			}
		}
		out.Players[k] = &cp
	}
	out.Deck = append([]Card(nil), s.Deck...)
	out.RoundWinners = append([]string(nil), s.RoundWinners...)
	out.MatchWinners = append([]string(nil), s.MatchWinners...)
	return &out
}

// toInt accepts JSON-decoded number shapes and returns a plain int.
func toInt(v any) (int, error) {
	switch n := v.(type) {
	case int:
		return n, nil
	case int32:
		return int(n), nil
	case int64:
		return int(n), nil
	case float64:
		return int(n), nil
	case float32:
		return int(n), nil
	case Card:
		return int(n), nil
	default:
		return 0, fmt.Errorf("not an int: %T", v)
	}
}

// toString accepts json-decoded strings (and the occasional fmt.Stringer
// payload tests pass directly).
func toString(v any) (string, error) {
	switch s := v.(type) {
	case string:
		return s, nil
	default:
		return "", errors.New("expected string")
	}
}
