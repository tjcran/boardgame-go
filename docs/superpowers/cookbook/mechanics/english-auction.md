# English ascending auction

> Players bid in turn order. Each bid must exceed the current high. A
> player can pass; passing drops them from the auction. When all but one
> player has passed, the last bidder wins and pays.

**Driving examples**

- Power Grid (power plants)
- Modern Art (one of four auction types)
- Furnace, Coal Baron
- Generally: any "going once, going twice, sold" mechanic

## How to compose it

- `core.ActivePlayersInOrder` (`core/stage.go`) cycles seats one-at-a-time,
  re-entering the same set until everyone but the winner has passed.
- `Events.EndStage` (`core/events.go`) is how a player drops out — calling
  it removes them from `ctx.ActivePlayers`.
- `economy.Pool.Spend` (`modules/economy/pool.go`) deducts the winning bid
  with `ErrInsufficient` protection.
- `ctx.ActivePlayers` shrinks to 1 → resolve in the phase's `EndIf` or
  the bid move's tail. The single remaining bidder wins.

## Sketch

```go
package auction

import (
    "fmt"
    "github.com/tjcran/boardgame-go/core"
    "github.com/tjcran/boardgame-go/modules/ccg"
    "github.com/tjcran/boardgame-go/modules/economy"
)

type G struct {
    *ccg.State
    LotID       ccg.EntityID
    HighBid     int
    HighBidder  string
    PoolFor     map[string]economy.Pool // playerID → gold pool
}

func New() *core.Game {
    return &core.Game{
        Name: "english-auction",
        MinPlayers: 2, MaxPlayers: 6,
        Setup: setup,
        Phases: map[string]*core.PhaseConfig{
            "auction": {
                OnBegin: startAuction,
                EndIf:   onlyOneBidderLeft,
                OnEnd:   awardLot,
                Moves: map[string]any{
                    "bid":  core.MoveFn(bid),
                    "pass": core.MoveFn(pass),
                },
            },
        },
        StartingPhase: "auction",
    }
}

func startAuction(mc *core.MoveContext) core.G {
    // Everyone bids in turn order, one move per re-entry; unlimited
    // re-entries until they pass.
    mc.Events.SetActivePlayers(
        core.ActivePlayersInOrder(mc.Ctx.PlayOrder, core.StageNull, 1, 1),
    )
    return mc.G
}

func bid(mc *core.MoveContext, args ...any) (core.G, error) {
    g := mc.G.(*G)
    if len(args) == 0 {
        return g, fmt.Errorf("%w: missing bid amount", core.ErrInvalidMove)
    }
    n, _ := args[0].(int)
    if n <= g.HighBid {
        return g, fmt.Errorf("%w: %d must exceed %d", core.ErrInvalidMove, n, g.HighBid)
    }
    pool := g.PoolFor[mc.PlayerID]
    if pool.Current(g.State) < n {
        return g, fmt.Errorf("%w: cannot afford %d", core.ErrInvalidMove, n)
    }
    g.HighBid, g.HighBidder = n, mc.PlayerID
    // Re-queue this player so they get another shot once others have bid.
    // ActivePlayersInOrder advances on MaxMoves; the Next chain repeats.
    return g, nil
}

func pass(mc *core.MoveContext, _ ...any) (core.G, error) {
    // EndStage drops *this* player from the active set permanently.
    mc.Events.EndStage()
    return mc.G, nil
}

func onlyOneBidderLeft(g core.G, ctx core.Ctx) bool {
    // Phase ends when ActivePlayers is at zero (everyone passed including
    // the high bidder — should not happen) OR has shrunk to one bidder
    // who is also the high bidder.
    if len(ctx.ActivePlayers) == 0 {
        return true
    }
    if len(ctx.ActivePlayers) == 1 {
        for pid := range ctx.ActivePlayers {
            if pid == g.(*G).HighBidder {
                return true
            }
        }
    }
    return false
}

func awardLot(mc *core.MoveContext) core.G {
    g := mc.G.(*G)
    if g.HighBidder == "" {
        return g // no bids; up to the game what to do
    }
    pool := g.PoolFor[g.HighBidder]
    _ = pool.Spend(g.State, g.HighBid) // pre-validated; ignore err
    // Hand the lot to the winner — game-specific. E.g. MoveTo into the
    // winner's tableau zone, then reset auction state for the next lot:
    g.HighBid, g.HighBidder, g.LotID = 0, "", 0
    return g
}
```

## Notes and pitfalls

- **`ActivePlayersInOrder` re-cycles automatically.** Each player gets
  `MaxMoves:1` per visit; once the chain reaches the end, the Next
  pointer is nil — so chain another `ActivePlayersInOrder` if you need
  another round, or wrap in a recursive helper. (Most English auctions
  end before going around twice, but tightly-contested lots do
  re-cycle.)
- **`pass` should call `EndStage`, not just return.** Returning advances
  the chain; calling `EndStage` *removes* the player from the active
  set so they aren't re-prompted next round.
- **Don't pre-deduct on bid.** Players who get outbid should keep their
  money; deduct only the winner in `OnEnd`. The `bid` move's affordability
  check is a guard, not a commit.
- **First bid edge case.** `HighBid == 0` at phase start means any
  positive bid is legal. If the lot has a minimum, initialise `HighBid`
  to `min - 1`.
- **Tie at zero bids.** If everyone passes, decide policy: lot returns
  to supply, leader takes free, or the auction repeats with a relisted
  lot. The `awardLot` hook is where this branches.

## Dutch (descending) auction variant

Dutch — clock ticks the price down and the first player to claim wins
— needs either:

1. A periodic server-driven move that decrements the price (requires a
   real-time scheduler; see the audit's deferred real-time-clock
   discussion in [README.md](README.md)).
2. A discrete-tick variant where each `tick` move drops the price by 1
   and is queued via `Events.RunMove` on every player input.

Neither is a clean fit for this pattern. For a typical board game's
Dutch flavour, treat it as a sealed-bid where the lowest claim-price wins
— that's the [sealed-bid pattern](sealed-bid-auction.md) with a different
resolver.

## See also

- `core/stage.go` — `ActivePlayersInOrder`, `ActivePlayersConfig`.
- `core/events.go` — `EndStage`, `EndPhase`, `SetActivePlayers`.
- `modules/economy/pool.go` — `Pool.Spend`, `ErrInsufficient`.
- `modules/economy/example_test.go` — `Pool` usage end-to-end.
