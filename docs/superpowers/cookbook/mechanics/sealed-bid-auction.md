# Sealed-bid auction

> Each player secretly writes one bid. All bids are revealed at once;
> highest wins and pays. Ties are resolved by a published rule.

**Driving examples**

- For Sale (round 1 — buying)
- Knizia auction games (Modern Art, Ra)
- Many euro-game scoring phases

This pattern is the [sealed-reveal](sealed-reveal.md) pattern with an
auction-specific resolver. Read sealed-reveal first; this page only
shows the differences.

## How to compose it

The reveal handler:

1. Iterates submissions in seat order to make tie-break deterministic.
2. Validates each bidder can afford their bid against their
   `economy.Pool` (drop the bid if not).
3. Picks the winning bid with the game's tie-break rule.
4. Deducts the winning bid via `Pool.Spend`.
5. Hands the lot to the winner (game-specific).

## Sketch

```go
package sealedbid

import (
    "github.com/tjcran/boardgame-go/core"
    "github.com/tjcran/boardgame-go/modules/ccg"
    "github.com/tjcran/boardgame-go/modules/economy"
)

type G struct {
    *ccg.State
    Secret  map[string]any   // {"players": {"<pid>": {"bid": int}}}
    Public  map[string]any   // current lot, scores, etc.
    Lots    []ccg.EntityID   // queue of lots to auction
    PoolFor map[string]economy.Pool
}

func New() *core.Game {
    return &core.Game{
        Name: "sealed-bid",
        MinPlayers: 2, MaxPlayers: 6,
        Setup: setup,
        PlayerView: maskOtherBids,
        Phases: map[string]*core.PhaseConfig{
            "bid": {
                OnBegin: func(mc *core.MoveContext) core.G {
                    mc.Events.SetActivePlayers(core.ActivePlayersAllOnce)
                    return mc.G
                },
                EndIf: func(_ core.G, ctx core.Ctx) bool {
                    return len(ctx.ActivePlayers) == 0
                },
                OnEnd: revealAndAward,
                Next:  "bid", // loop until Lots is empty; otherwise advance
                Moves: map[string]any{"submitBid": core.MoveFn(submitBid)},
            },
        },
        StartingPhase: "bid",
    }
}

func submitBid(mc *core.MoveContext, args ...any) (core.G, error) {
    g := mc.G.(*G)
    n, ok := args[0].(int)
    if !ok || n < 0 {
        return g, core.ErrInvalidMove
    }
    players := g.Secret["players"].(map[string]any)
    players[mc.PlayerID] = map[string]any{"bid": n}
    return g, nil
}

func revealAndAward(mc *core.MoveContext) core.G {
    g := mc.G.(*G)
    players := g.Secret["players"].(map[string]any)

    type entry struct {
        pid string
        bid int
    }
    var bids []entry
    for _, pid := range mc.Ctx.PlayOrder {
        sub, ok := players[pid].(map[string]any)
        if !ok { continue }
        b, _ := sub["bid"].(int)
        pool := g.PoolFor[pid]
        if pool.Current(g.State) < b {
            continue // can't afford → bid is void
        }
        bids = append(bids, entry{pid, b})
    }

    if len(bids) == 0 {
        return g // no valid bids; game decides what to do
    }
    winner := bids[0]
    for _, b := range bids[1:] {
        if b.bid > winner.bid {
            winner = b
        }
        // Tie-break: bids slice is already in seat order, so the
        // earlier seat keeps `winner`. Swap the comparator if your
        // game tie-breaks the other way.
    }

    pool := g.PoolFor[winner.pid]
    _ = pool.Spend(g.State, winner.bid)
    awardLot(g, winner.pid)
    g.Secret["players"] = map[string]any{}
    return g
}
```

## Notes and pitfalls

- **Affordability check at reveal, not at bid.** Otherwise a player could
  bid more than they hold "to bluff" — and your audit log shows
  inconsistent state. Reveal-time validation drops over-bids cleanly.
- **Tie-break must be deterministic.** Seat-order is the easy default
  (first to sit wins). Other options: random pick (use
  `mc.Random.Number(n)`); the last bidder loses; ties split the lot if
  the lot is divisible.
- **`StripSecrets` works if `G` is a `map[string]any`.** For struct-shaped
  `G` like the sketch above, write a custom `PlayerView` that copies
  `Secret["players"][requestingPlayer]` into the output and drops the
  rest:

  ```go
  func maskOtherBids(g core.G, _ core.Ctx, pid string) core.G {
      in := g.(*G)
      out := *in // shallow copy
      out.Secret = map[string]any{
          "players": map[string]any{
              pid: in.Secret["players"].(map[string]any)[pid],
          },
      }
      return &out
  }
  ```

- **Distinct bids per lot.** If you're auctioning multiple lots in one
  phase, key the per-lot submission map by `lotID` so resolving one lot
  doesn't accidentally clear bids for the next.

## See also

- [sealed-reveal.md](sealed-reveal.md) — parent pattern; covers the
  per-player hidden-state mechanics in more depth.
- [english-auction.md](english-auction.md) — the alternative when bids
  are visible and players can respond to each other.
- `modules/economy/pool.go` — `Pool.Spend`, `ErrInsufficient`.
- `core/playerview.go` — `StripSecrets` reference implementation; write a
  custom `PlayerView` for struct-shaped `G`.
