# Mechanics cookbook

Patterns for building common board-game mechanics out of existing engine
primitives — no new modules required.

Use this when designing a game and you want to know "is there already a way
to do X?". Each page answers:

- What the mechanic looks like (examples).
- Which primitives compose to express it.
- A minimal Go sketch.
- Where to look for the real implementations and tests.

These are *patterns*, not modules: small enough to copy into your game and
adapt. If you find yourself copy-pasting the same pattern across multiple
games, that is the signal to promote it into a module (see
`docs/superpowers/plans/` for examples of how that has gone before).

## Pages

- [sealed-reveal.md](sealed-reveal.md) — every player secretly submits an
  action, then a single reveal+resolve happens. (7 Wonders drafting,
  Diplomacy orders, sealed-bid lots.)
- [trick-taking.md](trick-taking.md) — round-robin card play with
  follow-suit constraint, winner takes the trick and leads next.
  (Hearts, Spades, Bridge.)
- [english-auction.md](english-auction.md) — ascending bids in turn order
  with passing; last bidder standing wins. (Power Grid, Modern Art.)
- [sealed-bid-auction.md](sealed-bid-auction.md) — sealed-reveal pattern
  combined with `economy.Pool` payment. (For Sale, Knizia bidding games.)

## Pattern-to-primitive map

| Pattern | Primary primitives |
|---|---|
| Sealed reveal | `core.ActivePlayersAllOnce`, `secret` key + `core.StripSecrets`, phase OnEnd |
| Trick-taking | `ccg.Zone` (hand, trick), default `TurnOrder`, custom move validation |
| English auction | `core.ActivePlayersInOrder`, `Events.EndStage`, `economy.Pool.Spend` |
| Sealed-bid auction | Sealed reveal + `economy.Pool.Spend` in reveal handler |

## Mechanics out of scope here

- **Area-control / influence scoring.** No primitive for region grouping
  or majority scoring — this is the one true gap identified in the
  module-coverage audit. When a driving game appears, that becomes a
  `tabletop.Regions` proposal, not a cookbook page.
- **Sub-turn real-time clocks.** Per-turn `TurnConfig.TimeBudget`
  exists, but true real-time (Galaxy Trucker, Captain Sonar) needs a
  server-side scheduler — out of module scope entirely.
