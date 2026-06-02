// Package hearts is a 4-player reference implementation of the classic
// trick-taking game on the boardgame-go engine. It exists primarily to
// validate the trick-taking cookbook page at
// docs/superpowers/cookbook/mechanics/trick-taking.md — by building a
// real game out of only the existing primitives the cookbook names.
//
// Scope is a single 13-trick hand with standard scoring:
//
//   - 2♣ leads the opening trick.
//   - Players must follow the lead suit if able.
//   - Hearts cannot be led until "broken" (played as a discard on a
//     non-hearts trick) — unless the leader has only hearts left.
//   - Each heart scores 1 penalty point; Q♠ scores 13.
//   - Game ends after the 13th trick; lowest score wins.
//
// Explicitly out of scope: passing phase, multi-hand matches to 100,
// shoot-the-moon. Those are mechanical add-ons and don't exercise any
// new primitives; the focus here is the trick-taking pattern itself.
package hearts

import (
	"fmt"

	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/modules/ccg"
)

// SetupData is the optional payload Game.Setup accepts. Pass Seed to
// pick a reproducible deal; zero means "shuffle off seed 0".
type SetupData struct {
	Seed uint64 `json:"seed"`
}

// State is the Hearts G payload. It embeds *ccg.State for entity/zone
// bookkeeping and adds the trick-state fields the trick-taking pattern
// keeps on G.
type State struct {
	*ccg.State

	// LeaderSeat is the PlayOrder index of whoever leads the next trick.
	// Set in Setup to the seat holding 2♣, and updated by resolveTrick.
	LeaderSeat int `json:"leaderSeat"`

	// Plays is the running trick — appended to by each play, cleared by
	// resolveTrick.
	Plays []TrickPlay `json:"plays,omitempty"`

	// LeadSuit is the suit established by the trick's leader. Empty
	// between tricks.
	LeadSuit string `json:"leadSuit,omitempty"`

	// HeartsBroken flips true the first time a heart is played as a
	// discard on a non-hearts trick. Once true, hearts may be led.
	HeartsBroken bool `json:"heartsBroken,omitempty"`

	// Scores accumulate penalty points across tricks. Lower is better.
	Scores map[string]int `json:"scores"`

	// TricksLeft starts at 13 and counts down to 0; gameEndIf fires at 0.
	TricksLeft int `json:"tricksLeft"`
}

// TrickPlay records one card played to the current trick.
type TrickPlay struct {
	PlayerID string       `json:"playerID"`
	CardID   ccg.EntityID `json:"cardID"`
	Suit     string       `json:"suit"`
	Rank     int          `json:"rank"`
}

const (
	deckZone  ccg.ZoneName = "deck"
	trickZ    ccg.ZoneName = "trick"
	suitClubs              = "C"
	suitDiams              = "D"
	suitHrts               = "H"
	suitSpds               = "S"
	rankQueen              = 12
)

var suits = []string{suitClubs, suitDiams, suitHrts, suitSpds}

func handZone(pid string) ccg.ZoneName { return ccg.ZoneName("hand_" + pid) }
func wonZone(pid string) ccg.ZoneName  { return ccg.ZoneName("won_" + pid) }

// New returns the registered Hearts game. Pass to a match.Manager.
func New() *core.Game {
	return &core.Game{
		Name:       "hearts",
		MinPlayers: 4,
		MaxPlayers: 4,
		Setup:      setup,
		Moves: map[string]any{
			"play": core.MoveFn(playCard),
		},
		Turn: &core.TurnConfig{
			MinMoves: 1,
			MaxMoves: 1,
			Order:    trickTurnOrder,
		},
		EndIf:     gameEndIf,
		Enumerate: enumerate,
	}
}

// setup builds the deck, deals 13 cards per seat off a deterministic
// seed (mirrors the loveletter pattern — Setup has no MoveContext, so
// Random is not yet available), and records the seat holding 2♣ as the
// opening leader.
func setup(ctx core.Ctx, raw any) core.G {
	seed := uint64(0)
	switch sd := raw.(type) {
	case SetupData:
		seed = sd.Seed
	case *SetupData:
		if sd != nil {
			seed = sd.Seed
		}
	case map[string]any:
		if v, ok := sd["seed"].(float64); ok {
			seed = uint64(v)
		}
	}

	s := ccg.NewState()
	s.NewZone(deckZone, true)
	s.NewZone(trickZ, true)
	for _, pid := range ctx.PlayOrder {
		s.NewZone(handZone(pid), false)
		s.NewZone(wonZone(pid), true)
	}

	// Build the 52-card deck.
	ids := make([]ccg.EntityID, 0, 52)
	for _, suit := range suits {
		for rank := 2; rank <= 14; rank++ {
			id := s.NewEntity("card", "", map[string]any{
				"suit": suit,
				"rank": rank,
			})
			_ = s.Add(deckZone, id)
			ids = append(ids, id)
		}
	}

	// Deterministic shuffle off the supplied seed.
	state := seed
	r := core.NewRandomFromState(&state)
	shuffled := core.Shuffle(r, ids)
	s.Zones[deckZone].Members = append([]ccg.EntityID(nil), shuffled...)

	// Deal round-robin and remember who holds 2♣.
	leaderSeat := 0
	for i := 0; i < 52; i++ {
		drawn, err := s.Draw(deckZone, 1)
		if err != nil {
			break
		}
		seat := i % len(ctx.PlayOrder)
		pid := ctx.PlayOrder[seat]
		cardID := drawn[0]
		_ = s.Add(handZone(pid), cardID)
		e := s.Entities[cardID]
		if e.AttrStr("suit", "") == suitClubs && e.AttrInt("rank", 0) == 2 {
			leaderSeat = seat
		}
	}

	scores := make(map[string]int, len(ctx.PlayOrder))
	for _, pid := range ctx.PlayOrder {
		scores[pid] = 0
	}

	return &State{
		State:      s,
		LeaderSeat: leaderSeat,
		Scores:     scores,
		TricksLeft: 13,
	}
}

// trickTurnOrder is the seat rotation. First seats the 2♣ holder at
// match start; Next either rotates to the next seat mid-trick, or
// returns the seat of the new leader once resolveTrick has updated
// LeaderSeat.
var trickTurnOrder = core.TurnOrder{
	First: func(mc *core.MoveContext) int {
		return mc.G.(*State).LeaderSeat
	},
	Next: func(mc *core.MoveContext) *int {
		g := mc.G.(*State)
		var next int
		if len(g.Plays) == 0 {
			next = g.LeaderSeat
		} else {
			last := g.Plays[len(g.Plays)-1].PlayerID
			next = (seatOf(last, mc.Ctx.PlayOrder) + 1) % len(mc.Ctx.PlayOrder)
		}
		return &next
	},
}

func seatOf(pid string, order []string) int {
	for i, p := range order {
		if p == pid {
			return i
		}
	}
	return 0
}

func suitOf(s *ccg.State, id ccg.EntityID) string {
	return s.Entities[id].AttrStr("suit", "")
}

func rankOf(s *ccg.State, id ccg.EntityID) int {
	return s.Entities[id].AttrInt("rank", 0)
}

// playCard is the only move. It enforces:
//
//   - Opening lead must be 2♣.
//   - When following, the lead suit must be played if the player has any.
//   - When leading, hearts cannot be led until broken — unless the
//     leader holds only hearts.
//
// If the trick is full after this play, resolveTrick runs in the same
// call.
func playCard(mc *core.MoveContext, args ...any) (core.G, error) {
	g := mc.G.(*State)
	if mc.PlayerID != mc.Ctx.CurrentPlayer {
		return g, fmt.Errorf("%w: not your turn", core.ErrInvalidMove)
	}
	if len(args) < 1 {
		return g, fmt.Errorf("%w: missing card id", core.ErrInvalidMove)
	}
	cardID, err := toEntityID(args[0])
	if err != nil {
		return g, fmt.Errorf("%w: %v", core.ErrInvalidMove, err)
	}

	hand := handZone(mc.PlayerID)
	if !g.Contains(hand, cardID) {
		return g, fmt.Errorf("%w: card not in your hand", core.ErrInvalidMove)
	}

	cardSuit := suitOf(g.State, cardID)
	cardRank := rankOf(g.State, cardID)

	// Opening lead constraint.
	if g.TricksLeft == 13 && len(g.Plays) == 0 {
		if !(cardSuit == suitClubs && cardRank == 2) {
			return g, fmt.Errorf("%w: opening lead must be 2♣", core.ErrInvalidMove)
		}
	}

	if len(g.Plays) == 0 {
		// Leading.
		if cardSuit == suitHrts && !g.HeartsBroken && !onlyHearts(g, hand) {
			return g, fmt.Errorf("%w: hearts not broken", core.ErrInvalidMove)
		}
	} else {
		// Following.
		if cardSuit != g.LeadSuit && hasSuit(g, hand, g.LeadSuit) {
			return g, fmt.Errorf("%w: must follow %s", core.ErrInvalidMove, g.LeadSuit)
		}
	}

	if err := g.MoveTo(cardID, trickZ); err != nil {
		return g, err
	}
	if len(g.Plays) == 0 {
		g.LeadSuit = cardSuit
	}
	if cardSuit == suitHrts {
		g.HeartsBroken = true
	}
	g.Plays = append(g.Plays, TrickPlay{
		PlayerID: mc.PlayerID,
		CardID:   cardID,
		Suit:     cardSuit,
		Rank:     cardRank,
	})

	if len(g.Plays) == len(mc.Ctx.PlayOrder) {
		resolveTrick(g, mc.Ctx.PlayOrder)
	}
	return g, nil
}

func hasSuit(g *State, zone ccg.ZoneName, suit string) bool {
	z, ok := g.Zones[zone]
	if !ok {
		return false
	}
	for _, id := range z.Members {
		if suitOf(g.State, id) == suit {
			return true
		}
	}
	return false
}

func onlyHearts(g *State, zone ccg.ZoneName) bool {
	z, ok := g.Zones[zone]
	if !ok || len(z.Members) == 0 {
		return false
	}
	for _, id := range z.Members {
		if suitOf(g.State, id) != suitHrts {
			return false
		}
	}
	return true
}

// resolveTrick determines the trick winner, transfers the trick cards
// to their "won" zone, applies penalty scoring (hearts + Q♠), updates
// LeaderSeat for the next trick, and clears trick state.
func resolveTrick(g *State, order []string) {
	winner := g.Plays[0]
	for _, p := range g.Plays[1:] {
		if p.Suit == g.LeadSuit && p.Rank > winner.Rank {
			winner = p
		}
	}
	points := 0
	for _, p := range g.Plays {
		_ = g.MoveTo(p.CardID, wonZone(winner.PlayerID))
		if p.Suit == suitHrts {
			points++
		}
		if p.Suit == suitSpds && p.Rank == rankQueen {
			points += 13
		}
	}
	g.Scores[winner.PlayerID] += points
	g.LeaderSeat = seatOf(winner.PlayerID, order)
	g.LeadSuit = ""
	g.Plays = nil
	g.TricksLeft--
}

// gameEndIf fires when the 13th trick has resolved. Returns a payload
// with the winner(s) (low-score) and final scores. Returns nil while
// the game is still in progress.
func gameEndIf(mc *core.MoveContext) any {
	s := mc.G.(*State)
	if s.TricksLeft > 0 {
		return nil
	}
	low := -1
	for _, v := range s.Scores {
		if low < 0 || v < low {
			low = v
		}
	}
	var winners []string
	for _, pid := range mc.Ctx.PlayOrder {
		if s.Scores[pid] == low {
			winners = append(winners, pid)
		}
	}
	return map[string]any{
		"winners": winners,
		"scores":  s.Scores,
	}
}

// enumerate lists every legal play card for the player on turn. Drives
// bots, replay viewers, and the MCP module bridge.
func enumerate(g core.G, ctx core.Ctx, playerID string) []core.EnumerateAction {
	s, ok := g.(*State)
	if !ok {
		return nil
	}
	if playerID != ctx.CurrentPlayer {
		return nil
	}
	hand, ok := s.Zones[handZone(playerID)]
	if !ok {
		return nil
	}

	leading := len(s.Plays) == 0
	mustFollow := !leading && hasSuit(s, handZone(playerID), s.LeadSuit)
	onlyH := leading && onlyHearts(s, handZone(playerID))
	openingLead := s.TricksLeft == 13 && leading

	out := make([]core.EnumerateAction, 0, len(hand.Members))
	for _, id := range hand.Members {
		suit := suitOf(s.State, id)
		rank := rankOf(s.State, id)
		switch {
		case openingLead:
			if !(suit == suitClubs && rank == 2) {
				continue
			}
		case mustFollow:
			if suit != s.LeadSuit {
				continue
			}
		case leading && suit == suitHrts && !s.HeartsBroken && !onlyH:
			continue
		}
		out = append(out, core.EnumerateAction{
			Move: "play",
			Args: []any{id},
		})
	}
	return out
}

func toEntityID(v any) (ccg.EntityID, error) {
	switch x := v.(type) {
	case ccg.EntityID:
		return x, nil
	case uint64:
		return ccg.EntityID(x), nil
	case int:
		if x >= 0 {
			return ccg.EntityID(x), nil
		}
	case int64:
		if x >= 0 {
			return ccg.EntityID(x), nil
		}
	case float64:
		if x >= 0 {
			return ccg.EntityID(uint64(x)), nil
		}
	}
	return 0, fmt.Errorf("not an entity id: %T", v)
}
