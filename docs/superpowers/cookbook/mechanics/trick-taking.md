# Trick-taking with follow-suit

> Round-robin card play, but each non-lead player must follow the lead
> suit if they have a card of that suit. Winner of the trick takes the
> cards and leads the next.

**Driving examples**

- Hearts (no trump, low-card wins certain tricks)
- Spades (fixed trump)
- Bridge (trump set by contract)
- Euchre, Pinochle, etc.

## How to compose it

- **Hands** as `ccg.Zone` (`modules/ccg/zone.go`) — one per player,
  unordered, populated from a `DeckList` at setup.
- **Trick area** as a single `ccg.Zone` that the trick winner empties at
  the end of each trick.
- **Lead suit / trump** as game-state fields on `G`.
- **Round-robin order** — the engine's default `TurnOrder` rotates seats;
  set the trick winner as the lead by overriding `PlayOrder` in the
  trick-resolution hook, or by using `TurnOrder.First` /
  `TurnOrder.Next` callbacks (`core/turn.go`).
- **Follow-suit constraint** — pure Go inside the `playCard` move:
  inspect the player's hand for the lead suit and return
  `core.ErrInvalidMove` if they have it but didn't play it.

No new primitives required.

## Sketch

```go
package trick

import (
    "fmt"
    "github.com/tjcran/boardgame-go/core"
    "github.com/tjcran/boardgame-go/modules/ccg"
)

type G struct {
    *ccg.State
    LeadSuit string                  // "" between tricks
    Trump    string                  // optional, "" = no trump
    Plays    []TrickPlay             // ordered by play
    Hands    map[string]ccg.ZoneName // playerID → hand zone
    TrickZ   ccg.ZoneName
}

type TrickPlay struct {
    PlayerID string
    CardID   ccg.EntityID
    Suit     string
    Rank     int
}

func suitOf(s *ccg.State, id ccg.EntityID) string {
    return s.Entities[id].AttrStr("suit", "")
}
func rankOf(s *ccg.State, id ccg.EntityID) int {
    return s.Entities[id].AttrInt("rank", 0)
}

// playCard validates follow-suit, moves the card into the trick zone,
// and — when the trick is full — resolves the winner.
func playCard(mc *core.MoveContext, args ...any) (core.G, error) {
    g := mc.G.(*G)
    cardID, ok := args[0].(ccg.EntityID)
    if !ok {
        return g, fmt.Errorf("%w: bad card id", core.ErrInvalidMove)
    }

    hand := g.Hands[mc.PlayerID]
    if !g.Contains(hand, cardID) {
        return g, fmt.Errorf("%w: card not in hand", core.ErrInvalidMove)
    }

    cardSuit := suitOf(g.State, cardID)
    if g.LeadSuit != "" && cardSuit != g.LeadSuit {
        // Reject if the player still holds a card of the lead suit.
        for _, h := range g.Zones[hand].Members {
            if suitOf(g.State, h) == g.LeadSuit {
                return g, fmt.Errorf("%w: must follow %s", core.ErrInvalidMove, g.LeadSuit)
            }
        }
    }
    if g.LeadSuit == "" {
        g.LeadSuit = cardSuit
    }

    if err := g.MoveTo(cardID, g.TrickZ); err != nil {
        return g, err
    }
    g.Plays = append(g.Plays, TrickPlay{
        PlayerID: mc.PlayerID, CardID: cardID,
        Suit: cardSuit, Rank: rankOf(g.State, cardID),
    })

    if len(g.Plays) == len(mc.Ctx.PlayOrder) {
        resolveTrick(mc, g)
    }
    return g, nil
}

func resolveTrick(mc *core.MoveContext, g *G) {
    winner := g.Plays[0]
    for _, p := range g.Plays[1:] {
        if beats(p, winner, g.LeadSuit, g.Trump) {
            winner = p
        }
    }
    // Move all trick cards to the winner's "won" pile zone here,
    // increment their score, etc. (omitted).

    // Set up the next trick: winner leads.
    g.LeadSuit = ""
    g.Plays = nil
    mc.Events.SetActivePlayers(core.ActivePlayersConfig{
        Value: map[string]string{winner.PlayerID: core.StageNull},
        Next:  rotationFrom(winner.PlayerID, mc.Ctx.PlayOrder),
    })
}

func beats(p, current TrickPlay, lead, trump string) bool {
    // Trump beats non-trump; otherwise highest rank in the lead suit
    // wins. Off-suit non-trump never beats anything.
    if p.Suit == trump && current.Suit != trump {
        return true
    }
    if p.Suit != current.Suit {
        return false
    }
    return p.Rank > current.Rank
}

// rotationFrom returns an ActivePlayersConfig chain that activates the
// remaining seats in play order, in the seat order *after* leader.
func rotationFrom(leader string, order []string) *core.ActivePlayersConfig {
    start := 0
    for i, p := range order { if p == leader { start = i; break } }
    rest := append([]string{}, order[start+1:]...)
    rest = append(rest, order[:start]...)
    if len(rest) == 0 { return nil }
    cfg := core.ActivePlayersInOrder(rest, core.StageNull, 1, 1)
    return &cfg
}
```

## Notes and pitfalls

- **Don't bake trump rules into the engine.** Some games (Hearts) don't
  use trump, some choose it dynamically (Bridge bidding sets it), some
  rotate (Euchre). Keep `Trump` on `G`; let `beats()` consult it.
- **Lead suit reset on trick resolution is essential.** Without it, the
  next leader's first card would be wrongly constrained to last trick's
  suit.
- **Card identity matters.** Suits and ranks belong on the
  `Entity.Attrs` map, not in a separate parallel structure — that way
  shuffling a deck doesn't desync with rank data, and the per-player
  view (if you want to hide hands) only has to redact `Zones[handFor(pid)]`.
- **Hiding hands** uses a custom `PlayerView` (since hands are
  per-player struct state, not a `map[string]any`). Iterate
  `g.Zones`, blank out `Members` for every hand that isn't the
  requesting player.
- **For games where every player plays the trick simultaneously**
  (no follow constraint), use the [sealed-reveal](sealed-reveal.md)
  pattern instead.

## When to promote to a module

If a second trick game lands in the repo, this is the candidate to
extract: a `modules/trick` package with `Trick{LeadSuit, Trump, Plays}`,
`Play(handZone, cardID, extractSuit) error`, and `Resolve(comparator)
(winnerPID, taken []EntityID)`. Estimated 150 LOC. Until then, copying
~50 lines into each game is cheaper than designing the abstraction.

## See also

- `modules/ccg/zone.go` — Zone API (`Contains`, `MoveTo`, `Draw`,
  `Shuffle`).
- `modules/ccg/entity.go` — `Entity.Attrs` for suit/rank.
- `core/turn.go` — `TurnOrder.First` / `Next` if you need stronger control
  over who plays when.
- `core/stage.go` — `ActivePlayersInOrder` for the next-leader rotation.
