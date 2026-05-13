package loveletter

import (
	"errors"
	"testing"

	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/testhelpers"
)

// scenario starts a match in the given player-keyed hands/deck setup
// with the named active player. Callers supply *post-draw* hand sizes
// for the current player (2 cards) and pre-draw for others (1 card).
//
// Tokens defaults to 0 unless an entry is supplied via opts.
func scenario(t *testing.T, opts scenarioOpts) (core.State, *core.Game) {
	t.Helper()
	g := New()
	if opts.players == 0 {
		opts.players = 2
	}
	playOrder := defaultOrder(opts.players)
	st := &State{
		Players:     make(map[string]*PlayerState, opts.players),
		Deck:        append([]Card(nil), opts.deck...),
		Burned:      opts.burned,
		Round:       maxInt(1, opts.round),
		TokensToWin: opts.tokensToWin,
	}
	if st.TokensToWin == 0 {
		st.TokensToWin = tokensToWin(opts.players)
	}
	for _, pid := range playOrder {
		ps := &PlayerState{Alive: true}
		if h, ok := opts.hands[pid]; ok {
			ps.Hand = append([]Card(nil), h...)
		}
		if tk, ok := opts.tokens[pid]; ok {
			ps.Tokens = tk
		}
		if d, ok := opts.dead[pid]; ok && d {
			ps.Alive = false
		}
		if p, ok := opts.protected[pid]; ok && p {
			ps.Protected = true
		}
		st.Players[pid] = ps
	}
	state := testhelpers.Scenario(g, st, testhelpers.CtxOverrides{
		NumPlayers:    opts.players,
		CurrentPlayer: opts.active,
	})
	return state, g
}

type scenarioOpts struct {
	players     int                // 2..4
	active      string             // current player ID; default "0"
	hands       map[string][]Card  // per-player hand cards
	deck        []Card             // remaining deck (top is index 0)
	burned      Card               // burned card (set CardNone to skip)
	round       int                // default 1
	tokens      map[string]int     // per-player token counts
	dead        map[string]bool    // per-player eliminated-already flag
	protected   map[string]bool    // per-player Handmaid-protected flag
	tokensToWin int                // override match-end threshold
}

func defaultOrder(n int) []string {
	out := make([]string, n)
	for i := 0; i < n; i++ {
		out[i] = itoa(i)
	}
	return out
}

func itoa(n int) string {
	switch n {
	case 0:
		return "0"
	case 1:
		return "1"
	case 2:
		return "2"
	case 3:
		return "3"
	}
	return ""
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func apply(t *testing.T, g *core.Game, s core.State, pid, move string, args ...any) core.State {
	t.Helper()
	next, err := core.Apply(g, s, core.MoveRequest{
		PlayerID: pid,
		Move:     move,
		Args:     args,
	})
	if err != nil {
		t.Fatalf("%s by %s: %v", move, pid, err)
	}
	return next
}

func applyErr(t *testing.T, g *core.Game, s core.State, pid, move string, args ...any) error {
	t.Helper()
	_, err := core.Apply(g, s, core.MoveRequest{
		PlayerID: pid,
		Move:     move,
		Args:     args,
	})
	return err
}

// ---------------------------------------------------------------------
// Guard
// ---------------------------------------------------------------------

func TestGuardCorrectGuessEliminates(t *testing.T) {
	s, g := scenario(t, scenarioOpts{
		active: "0",
		hands: map[string][]Card{
			"0": {CardGuard, CardPriest},
			"1": {CardPriest},
		},
		deck: []Card{CardBaron, CardHandmaid},
	})
	next := apply(t, g, s, "0", "playGuard", "1", int(CardPriest))
	st := next.G.(*State)
	// Round ends because P0 is the only survivor; tokens awarded and
	// the round resets — eliminated/alive flags get cleared by the
	// reset, so we assert on the round outcome instead.
	if st.Players["0"].Tokens != 1 {
		t.Fatalf("expected P0 tokens=1, got %d", st.Players["0"].Tokens)
	}
	if len(st.RoundWinners) != 1 || st.RoundWinners[0] != "0" {
		t.Fatalf("expected RoundWinners=[0], got %v", st.RoundWinners)
	}
}

func TestGuardWrongGuessNoEffect(t *testing.T) {
	s, g := scenario(t, scenarioOpts{
		active: "0",
		hands: map[string][]Card{
			"0": {CardGuard, CardPriest},
			"1": {CardBaron},
		},
		deck: []Card{CardHandmaid, CardKing, CardPrince}, // enough for opponent to draw
	})
	next := apply(t, g, s, "0", "playGuard", "1", int(CardPriest))
	st := next.G.(*State)
	if !st.Players["1"].Alive {
		t.Fatalf("expected player 1 still alive after wrong guess")
	}
	if st.Players["0"].Tokens != 0 {
		t.Fatalf("no tokens awarded mid-round; got %d", st.Players["0"].Tokens)
	}
}

func TestGuardCannotNameGuard(t *testing.T) {
	s, g := scenario(t, scenarioOpts{
		active: "0",
		hands: map[string][]Card{
			"0": {CardGuard, CardPriest},
			"1": {CardPriest},
		},
		deck: []Card{CardBaron, CardHandmaid},
	})
	err := applyErr(t, g, s, "0", "playGuard", "1", int(CardGuard))
	if !errors.Is(err, core.ErrInvalidMove) {
		t.Fatalf("expected ErrInvalidMove for naming Guard, got %v", err)
	}
}

// ---------------------------------------------------------------------
// Priest
// ---------------------------------------------------------------------

func TestPriestRevealsToActorOnly(t *testing.T) {
	s, g := scenario(t, scenarioOpts{
		active: "0",
		hands: map[string][]Card{
			"0": {CardPriest, CardHandmaid},
			"1": {CardKing},
		},
		deck: []Card{CardGuard, CardGuard},
	})
	next := apply(t, g, s, "0", "playPriest", "1")
	st := next.G.(*State)
	if got, want := st.Players["0"].SeenLastPriest["1"], CardKing; got != want {
		t.Fatalf("actor SeenLastPriest[1]: got %s, want %s", got, want)
	}

	// The PlayerView for seat 1 must hide P0's SeenLastPriest.
	view := playerView(st, next.Ctx, "1").(*State)
	if view.Players["0"].SeenLastPriest != nil {
		t.Fatalf("PlayerView for non-owner leaked SeenLastPriest: %v", view.Players["0"].SeenLastPriest)
	}
	// And the owner's own view should still see it.
	ownView := playerView(st, next.Ctx, "0").(*State)
	if ownView.Players["0"].SeenLastPriest["1"] != CardKing {
		t.Fatalf("PlayerView for owner stripped SeenLastPriest")
	}
}

// ---------------------------------------------------------------------
// Baron
// ---------------------------------------------------------------------

func TestBaronEliminatesLowerHand(t *testing.T) {
	s, g := scenario(t, scenarioOpts{
		active: "0",
		hands: map[string][]Card{
			"0": {CardBaron, CardKing}, // King(6) vs Guard(1) — P0 wins
			"1": {CardGuard},
		},
		deck: []Card{CardHandmaid, CardPrince},
	})
	next := apply(t, g, s, "0", "playBaron", "1")
	st := next.G.(*State)
	// P1 lost the comparison → eliminated → P0 is the sole survivor →
	// round ends and P0 earns a token. (After round reset everyone is
	// alive again, so token count is the observable outcome.)
	if st.Players["0"].Tokens != 1 {
		t.Fatalf("expected P0 tokens=1 after Baron win, got %d", st.Players["0"].Tokens)
	}
	if len(st.RoundWinners) != 1 || st.RoundWinners[0] != "0" {
		t.Fatalf("expected RoundWinners=[0], got %v", st.RoundWinners)
	}
}

func TestBaronTieNoEffect(t *testing.T) {
	s, g := scenario(t, scenarioOpts{
		active: "0",
		hands: map[string][]Card{
			"0": {CardBaron, CardKing},
			"1": {CardKing},
		},
		deck: []Card{CardHandmaid, CardPrince},
	})
	next := apply(t, g, s, "0", "playBaron", "1")
	st := next.G.(*State)
	if !st.Players["0"].Alive || !st.Players["1"].Alive {
		t.Fatalf("expected both alive on Baron tie; got %+v / %+v",
			st.Players["0"].Alive, st.Players["1"].Alive)
	}
}

// ---------------------------------------------------------------------
// Countess-forcing rule
// ---------------------------------------------------------------------

func TestCountessForcesAgainstKing(t *testing.T) {
	s, g := scenario(t, scenarioOpts{
		active: "0",
		hands: map[string][]Card{
			"0": {CardCountess, CardKing},
			"1": {CardGuard},
		},
		deck: []Card{CardHandmaid, CardPrince},
	})
	err := applyErr(t, g, s, "0", "playKing", "1")
	if !errors.Is(err, core.ErrInvalidMove) {
		t.Fatalf("expected Countess-forcing rejection, got %v", err)
	}
}

func TestCountessForcesAgainstPrince(t *testing.T) {
	s, g := scenario(t, scenarioOpts{
		active: "0",
		hands: map[string][]Card{
			"0": {CardCountess, CardPrince},
			"1": {CardGuard},
		},
		deck: []Card{CardHandmaid, CardKing},
	})
	err := applyErr(t, g, s, "0", "playPrince", "1")
	if !errors.Is(err, core.ErrInvalidMove) {
		t.Fatalf("expected Countess-forcing rejection, got %v", err)
	}
}

func TestCountessNotForcedByOtherHand(t *testing.T) {
	// Countess paired with Guard: optional, not forced.
	s, g := scenario(t, scenarioOpts{
		active: "0",
		hands: map[string][]Card{
			"0": {CardCountess, CardGuard},
			"1": {CardPriest},
		},
		deck: []Card{CardHandmaid, CardKing},
	})
	if _, err := core.Apply(g, s, core.MoveRequest{PlayerID: "0", Move: "playGuard", Args: []any{"1", int(CardPriest)}}); err != nil {
		t.Fatalf("Guard play should be legal with Countess+Guard: %v", err)
	}
}

// ---------------------------------------------------------------------
// Handmaid
// ---------------------------------------------------------------------

func TestHandmaidProtectsFromGuard(t *testing.T) {
	// P0 plays Handmaid → P0 protected. Then in next turn P1 tries Guard
	// on P0 with the right card name — should resolve to no effect.
	s, g := scenario(t, scenarioOpts{
		active: "0",
		hands: map[string][]Card{
			"0": {CardHandmaid, CardKing},
			"1": {CardGuard},
		},
		deck: []Card{CardPriest, CardBaron, CardPrince},
	})
	s2 := apply(t, g, s, "0", "playHandmaid")
	// Validate Handmaid set Protected and turn advanced to P1.
	st2 := s2.G.(*State)
	if !st2.Players["0"].Protected {
		t.Fatalf("expected P0 Protected after Handmaid")
	}
	if s2.Ctx.CurrentPlayer != "1" {
		t.Fatalf("expected current player 1 after P0's turn, got %s", s2.Ctx.CurrentPlayer)
	}
	// P1 had 1 card (Guard); Turn.OnBegin drew another card on P1.
	// They play Guard on protected P0 — naming King (P0 holds King) —
	// should resolve to no effect, P0 still alive.
	s3 := apply(t, g, s2, "1", "playGuard", "0", int(CardKing))
	st3 := s3.G.(*State)
	if !st3.Players["0"].Alive {
		t.Fatalf("Handmaid-protected P0 was eliminated by Guard; expected no effect")
	}
}

func TestHandmaidExpiresOnOwnerTurn(t *testing.T) {
	s, g := scenario(t, scenarioOpts{
		active: "0",
		hands: map[string][]Card{
			"0": {CardHandmaid, CardKing},
			"1": {CardPriest},
		},
		deck: []Card{CardGuard, CardBaron, CardPrince},
	})
	// P0 plays Handmaid → P0 protected; engine advances to P1.
	s2 := apply(t, g, s, "0", "playHandmaid")
	// P1 plays Priest on P0 (Handmaid still active) — no reveal.
	s3 := apply(t, g, s2, "1", "playPriest", "0")
	// Now P0's turn starts again; Turn.OnBegin should clear Protected.
	st3 := s3.G.(*State)
	if st3.Players["0"].Protected {
		t.Fatalf("expected Protected cleared at start of P0's next turn")
	}
}

// ---------------------------------------------------------------------
// Prince
// ---------------------------------------------------------------------

func TestPrinceForcesPrincessDiscardEliminates(t *testing.T) {
	s, g := scenario(t, scenarioOpts{
		active: "0",
		hands: map[string][]Card{
			"0": {CardPrince, CardGuard},
			"1": {CardPrincess},
		},
		deck: []Card{CardKing, CardHandmaid},
	})
	next := apply(t, g, s, "0", "playPrince", "1")
	st := next.G.(*State)
	// Prince forced P1 to discard Princess → eliminated → round ends
	// with P0 sole survivor.
	if st.Players["0"].Tokens != 1 {
		t.Fatalf("expected P0 to win round, tokens=1; got %d", st.Players["0"].Tokens)
	}
}

func TestPrinceDrawsBurnedCardWhenDeckEmpty(t *testing.T) {
	s, g := scenario(t, scenarioOpts{
		active: "0",
		players: 3, // 3-player avoids immediate round-end on single elim
		hands: map[string][]Card{
			"0": {CardPrince, CardGuard},
			"1": {CardBaron},
			"2": {CardHandmaid},
		},
		deck:   nil, // empty deck
		burned: CardKing,
	})
	next := apply(t, g, s, "0", "playPrince", "1")
	st := next.G.(*State)
	// After Prince: P1 discards Baron and draws the burned King.
	// Deck is still empty → round ends. Highest hand wins:
	// P0=Guard(1), P1=King(6), P2=Handmaid(4) → P1 wins.
	// Token of affection goes to P1; observable post-reset.
	if st.Players["1"].Tokens != 1 {
		t.Fatalf("expected P1 to win round via burned-card draw, tokens=1; got %d", st.Players["1"].Tokens)
	}
	if len(st.RoundWinners) != 1 || st.RoundWinners[0] != "1" {
		t.Fatalf("expected RoundWinners=[1], got %v", st.RoundWinners)
	}
}

// ---------------------------------------------------------------------
// King
// ---------------------------------------------------------------------

func TestKingSwapsHands(t *testing.T) {
	s, g := scenario(t, scenarioOpts{
		active: "0",
		hands: map[string][]Card{
			"0": {CardKing, CardGuard},
			"1": {CardPriest},
		},
		deck: []Card{CardBaron, CardHandmaid, CardPrince},
	})
	next := apply(t, g, s, "0", "playKing", "1")
	st := next.G.(*State)
	// After King resolves: actor holds the Priest; target holds the
	// Guard. Then turn advances and Turn.OnBegin draws a card for
	// the new current player (P1). So P1's hand becomes [Guard, draw].
	if len(st.Players["0"].Hand) != 1 || st.Players["0"].Hand[0] != CardPriest {
		t.Fatalf("expected P0 hand=[Priest], got %v", st.Players["0"].Hand)
	}
	if len(st.Players["1"].Hand) != 2 {
		t.Fatalf("expected P1 hand size 2 after OnBegin draw, got %v", st.Players["1"].Hand)
	}
	if st.Players["1"].Hand[0] != CardGuard {
		t.Fatalf("expected P1's first card to be the swapped Guard, got %v", st.Players["1"].Hand[0])
	}
}

// ---------------------------------------------------------------------
// Princess
// ---------------------------------------------------------------------

func TestPrincessSelfPlayEliminates(t *testing.T) {
	s, g := scenario(t, scenarioOpts{
		active: "0",
		hands: map[string][]Card{
			"0": {CardPrincess, CardGuard},
			"1": {CardBaron},
		},
		deck: []Card{CardHandmaid, CardPrince},
	})
	next := apply(t, g, s, "0", "playPrincess")
	st := next.G.(*State)
	// P0 voluntarily eliminates themselves → P1 sole survivor → P1
	// wins the round.
	if st.Players["1"].Tokens != 1 {
		t.Fatalf("expected P1 tokens=1 after P0 self-elim, got %d", st.Players["1"].Tokens)
	}
}

// ---------------------------------------------------------------------
// Round + match end
// ---------------------------------------------------------------------

func TestDeckEmptyEndsRoundOnHighestHand(t *testing.T) {
	// After P0 plays their move, the deck will be empty. Turn.OnEnd
	// should detect depletion and award the round to the highest
	// remaining hand.
	s, g := scenario(t, scenarioOpts{
		active: "0",
		hands: map[string][]Card{
			"0": {CardCountess, CardGuard}, // P0 plays Countess → keeps Guard(1)
			"1": {CardKing},                 // P1 keeps King(6)
		},
		deck: nil, // no draw available at next turn
	})
	next := apply(t, g, s, "0", "playCountess")
	st := next.G.(*State)
	if len(st.RoundWinners) != 1 || st.RoundWinners[0] != "1" {
		t.Fatalf("expected RoundWinners=[1] (King beats Guard), got %v", st.RoundWinners)
	}
	if st.Players["1"].Tokens != 1 {
		t.Fatalf("expected P1 tokens=1, got %d", st.Players["1"].Tokens)
	}
}

func TestMatchEndsAtTokenThreshold(t *testing.T) {
	// Two-player game: tokensToWin=7. Set P0 at 6 tokens; finishing
	// this round wins.
	s, g := scenario(t, scenarioOpts{
		active: "0",
		hands: map[string][]Card{
			"0": {CardGuard, CardPriest},
			"1": {CardPriest},
		},
		tokens: map[string]int{"0": 6},
		deck:   []Card{CardKing, CardHandmaid, CardPrince},
	})
	next := apply(t, g, s, "0", "playGuard", "1", int(CardPriest))
	st := next.G.(*State)
	if !st.MatchOver {
		t.Fatalf("expected MatchOver=true after threshold-crossing token award")
	}
	if len(st.MatchWinners) != 1 || st.MatchWinners[0] != "0" {
		t.Fatalf("expected MatchWinners=[0], got %v", st.MatchWinners)
	}
	if next.Ctx.Gameover == nil {
		t.Fatalf("expected ctx.Gameover populated by Game.EndIf")
	}
}

func TestNextRoundStartsAtWinner(t *testing.T) {
	// In a 3-player game, P1 wins round 1 by surviving. Round 2's first
	// turn should be P1, not P0.
	s, g := scenario(t, scenarioOpts{
		players: 3,
		active:  "0",
		hands: map[string][]Card{
			"0": {CardPrincess, CardGuard}, // P0 about to suicide
			"1": {CardKing},
			"2": {CardHandmaid}, // P2 has Handmaid, but won't get to play it
		},
		// P2 will be eliminated by P1 before deck depletion to leave
		// P1 as sole survivor. Easier path: P0 plays Princess →
		// eliminated; then P1's turn, P1 plays Guard correctly named
		// against P2. But P1 doesn't hold Guard here.
		//
		// Simpler scenario: P0 has Baron + King; P0 plays Baron on
		// P2 — wait, we want P1 to win, not P0. Adjust:
		deck: []Card{CardBaron, CardBaron, CardGuard, CardHandmaid, CardPrince, CardPrince, CardPriest, CardPriest},
	})
	// P0 plays Princess (suicide). After: P0 eliminated; round
	// continues to P1's turn (Turn.OnBegin draws). P1 then needs to
	// eliminate P2. Easiest: rig the scenario so P1 has Baron pre-loaded.
	// Restart with cleaner state:

	s, g = scenario(t, scenarioOpts{
		players: 3,
		active:  "0",
		hands: map[string][]Card{
			"0": {CardPrincess, CardGuard},
			"1": {CardKing},
			"2": {CardGuard},
		},
		// Deck: P1 will draw Baron next; then P1 plays Baron on P2 →
		// P1 holds King(6) vs P2 Guard(1) → P2 eliminated → P1 sole
		// survivor → round ends → P1 wins round, becomes NextStarter.
		// Round 2 begins: Setup-style reset shuffles a new deck via
		// mc.Random; OnBegin draws for P1 first.
		deck: []Card{CardBaron, CardHandmaid, CardPrince, CardPriest, CardPriest, CardCountess},
	})
	s = apply(t, g, s, "0", "playPrincess") // P0 dies
	st1 := s.G.(*State)
	if st1.Players["0"].Alive {
		t.Fatalf("P0 should be dead after Princess play")
	}
	if s.Ctx.CurrentPlayer != "1" {
		t.Fatalf("expected current player 1 after P0 elimination, got %s", s.Ctx.CurrentPlayer)
	}

	// P1's turn. After OnBegin draw, P1 has [King, Baron].
	s = apply(t, g, s, "1", "playBaron", "2")
	st2 := s.G.(*State)
	// P2 lost the Baron comparison → P1 is sole survivor → round ends
	// → round 2 begins with P1 as starter.
	if st2.Players["1"].Tokens != 1 {
		t.Fatalf("expected P1 to win the round, tokens=1; got %d", st2.Players["1"].Tokens)
	}
	if st2.Round != 2 {
		t.Fatalf("expected round 2 after round-end transition, got round=%d", st2.Round)
	}
	if s.Ctx.CurrentPlayer != "1" {
		t.Fatalf("expected round 2 to begin with P1 (last round's winner), got %s", s.Ctx.CurrentPlayer)
	}
}

// ---------------------------------------------------------------------
// PlayerView redactions
// ---------------------------------------------------------------------

func TestPlayerViewHidesOtherHandsAndDeck(t *testing.T) {
	s, _ := scenario(t, scenarioOpts{
		active: "0",
		hands: map[string][]Card{
			"0": {CardGuard, CardPriest},
			"1": {CardKing},
		},
		deck:   []Card{CardCountess, CardPrincess},
		burned: CardHandmaid,
	})
	st := s.G.(*State)
	view0 := playerView(st, s.Ctx, "0").(*State)
	if len(view0.Deck) != 0 {
		t.Fatalf("PlayerView leaked Deck contents: %v", view0.Deck)
	}
	if view0.Burned != CardNone {
		t.Fatalf("PlayerView leaked Burned card: %v", view0.Burned)
	}
	if len(view0.Players["1"].Hand) != 0 {
		t.Fatalf("PlayerView leaked opponent's hand: %v", view0.Players["1"].Hand)
	}
	if len(view0.Players["0"].Hand) != 2 {
		t.Fatalf("PlayerView for self should retain own hand; got %v", view0.Players["0"].Hand)
	}

	// Spectator view sees no hands.
	specView := playerView(st, s.Ctx, "").(*State)
	for pid, p := range specView.Players {
		if len(p.Hand) > 0 {
			t.Fatalf("Spectator PlayerView leaked %s hand: %v", pid, p.Hand)
		}
	}
}
