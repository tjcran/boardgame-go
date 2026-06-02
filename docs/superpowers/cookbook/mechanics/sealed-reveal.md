# Sealed reveal

> Every player privately submits an action; once all submissions are in,
> the engine reveals them and resolves in a single deterministic step.

**Driving examples**

- 7 Wonders draft picks
- Diplomacy order submission
- Race for the Galaxy role pick
- Any sealed-bid auction (see [sealed-bid-auction.md](sealed-bid-auction.md))

## How to compose it

Two primitives carry the weight:

1. `core.ActivePlayersAllOnce` (in `core/stage.go`) — adds every player to
   `ctx.ActivePlayers` with `MinMoves:1, MaxMoves:1`, so each player may
   move exactly once. When the last player moves, the set drains to
   empty.
2. The `secret` key + `core.StripSecrets` (in `core/playerview.go`) —
   anything stored under `G["secret"]` is stripped before delivery to
   clients; the `players` map is filtered to the requesting player. Game
   state is in cleartext on the server, hidden on the wire.

The reveal+resolve step runs in the phase's `OnEnd` hook (in
`core/phase.go`) when `ctx.ActivePlayers` is empty, or in `TurnConfig.EndIf`
returning true when the same condition holds.

## Sketch

```go
package draft

import "github.com/tjcran/boardgame-go/core"

// G shape:
//   {
//     "secret": { "players": { "<pid>": { "submission": <pick> } } },
//     "public": { ... whatever is already revealed ... },
//   }

func New() *core.Game {
    return &core.Game{
        Name: "draft",
        MinPlayers: 3, MaxPlayers: 7,
        Setup: func(_ core.Ctx, _ any) core.G {
            return map[string]any{
                "secret": map[string]any{
                    "players": map[string]any{},
                },
                "public": map[string]any{ /* shared market, scores, etc. */ },
            }
        },
        PlayerView: core.StripSecrets,
        Phases: map[string]*core.PhaseConfig{
            "submit": {
                OnBegin: func(mc *core.MoveContext) core.G {
                    mc.Events.SetActivePlayers(core.ActivePlayersAllOnce)
                    return mc.G
                },
                EndIf: func(g core.G, ctx core.Ctx) bool {
                    return len(ctx.ActivePlayers) == 0
                },
                OnEnd:    revealAndResolve,
                Next:     "submit", // loop back; or "score" when round count reached
                Moves:    map[string]any{"submit": core.MoveFn(submit)},
            },
        },
        StartingPhase: "submit",
    }
}

func submit(mc *core.MoveContext, args ...any) (core.G, error) {
    if len(args) == 0 {
        return mc.G, core.ErrInvalidMove
    }
    g := mc.G.(map[string]any)
    secret := g["secret"].(map[string]any)
    players := secret["players"].(map[string]any)
    players[mc.PlayerID] = map[string]any{"submission": args[0]}
    return g, nil
}

// revealAndResolve runs once, when every player has submitted exactly one
// move. ActivePlayers is empty here; that is the trigger.
func revealAndResolve(mc *core.MoveContext) core.G {
    g := mc.G.(map[string]any)
    secret := g["secret"].(map[string]any)
    players := secret["players"].(map[string]any)

    // Apply in seat order so resolution is deterministic.
    for _, pid := range mc.Ctx.PlayOrder {
        sub, ok := players[pid].(map[string]any)
        if !ok { continue }
        apply(g, pid, sub["submission"])
    }
    // Clear submissions for the next round.
    secret["players"] = map[string]any{}
    return g
}

func apply(g map[string]any, playerID string, pick any) {
    // game-specific: move the picked card from the public market into
    // the player's tableau, score it, etc.
}
```

## Notes and pitfalls

- **The trigger is `len(ctx.ActivePlayers) == 0`.** Don't track a separate
  "submitted count" — it's redundant and will desync with the
  authoritative active-set state.
- **`StripSecrets` only works on `map[string]any` shapes.** If you have a
  struct-shaped `G`, write a small custom `PlayerView` that masks the same
  fields (see `core/playerview_test.go` for the shape).
- **A submit move with `MaxMoves:1` is rejected after the first call** —
  no need to defend against double-submit inside the move; the reducer
  handles it. If you *want* a player to overwrite their own pick, set
  `MaxMoves:0` and trigger reveal explicitly when every player has a
  submission key.
- **Submission *existence* leaks even when content doesn't.** That is
  usually fine (other players can see "Alice has submitted"). If you want
  to hide even existence, write `players` keys you control rather than
  using the `players` map directly.
- **Reveal order matters.** Resolve in `mc.Ctx.PlayOrder` (seat order) or
  some other stable list — never iterate `players` directly, since Go map
  iteration is randomized.

## See also

- `core/stage.go` — `ActivePlayersAllOnce`, `ActivePlayersConfig`.
- `core/playerview.go` — `StripSecrets` source.
- `core/bgio_more_test.go` — `TestBGIO478_ActivePlayersInOrderCyclesThroughList`
  and adjacent tests exercise the active-player lifecycle this pattern
  relies on.
- `core/stage_test.go` — examples of `SetActivePlayers(Others)` and
  per-player stage moves.
