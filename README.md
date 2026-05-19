# boardgame-go

A Go-based engine and authoritative server for turn-based board games,
card games, deckbuilders, miniatures wargames, and auto-battlers.
Inspired by
[boardgame.io](https://github.com/boardgameio/boardgame.io); structurally
a superset of its non-React surface, plus the features Go's runtime and
type system make easy that JavaScript's don't.

> **Status:** production-shaped. Full BGIO parity for the engine,
> plugins, and lobby; 22+ open BGIO issues addressed (closed in our
> port); four opt-in `modules/` libraries on top — `ccg/` (CCG/TCG
> bookkeeping), `tabletop/` (wargame spatial + dice), `economy/`
> (turn-resource pools), `shop/` (refreshable market). See
> [PARITY.md](PARITY.md), [GO_LEVERAGE.md](GO_LEVERAGE.md), and
> [JS_LIMITATIONS.md](JS_LIMITATIONS.md) for the inventory.

## Why

If you want to host turn-based games from a Go service — alongside your
existing APIs, behind your own auth, deployed as a single static
binary — boardgame.io's JavaScript-only stack forces a polyglot
deployment. This is a Go-native take on the same ideas:

- **Declarative game definitions.** A `Game` is data: `Setup`, `Moves`,
  `Phases`, `Turn`, `EndIf`. The framework is the reducer.
- **Authoritative server.** Server owns canonical state; clients submit
  moves with credentials; server validates, runs the reducer, advances
  the turn, broadcasts.
- **Pure reducers.** Moves are `func(*MoveContext, ...any) (G, error)`.
  Deterministic given inputs. Unit-test rules without spinning up a
  server.
- **Cross-match concurrency.** Goroutines. Different matches run in
  parallel on the same process; manager overhead is flat across
  1–64 concurrent matches (see `bench/`).

## Quick start

```bash
go install github.com/tjcran/boardgame-go/cmd/boardgame-server@latest
boardgame-server -addr :8080
```

The reference binary serves the bundled tic-tac-toe game. In another
shell:

```bash
# Create a match
MATCH=$(curl -s -X POST localhost:8080/games/tic-tac-toe/create -d '{}' \
  | jq -r .matchID)

# Seat two players. The response carries playerCredentials — required
# on every subsequent action.
ALICE=$(curl -s -X POST localhost:8080/games/tic-tac-toe/$MATCH/join \
  -d '{"playerName":"alice"}')
ALICE_ID=$(jq -r .playerID <<<"$ALICE")
ALICE_CR=$(jq -r .playerCredentials <<<"$ALICE")

BOB=$(curl -s -X POST localhost:8080/games/tic-tac-toe/$MATCH/join \
  -d '{"playerName":"bob"}')
BOB_ID=$(jq -r .playerID <<<"$BOB")
BOB_CR=$(jq -r .playerCredentials <<<"$BOB")

# Alice plays cell 4 (centre)
curl -X POST localhost:8080/games/tic-tac-toe/$MATCH/move \
  -d "{\"playerID\":\"$ALICE_ID\",\"credentials\":\"$ALICE_CR\",\"move\":\"clickCell\",\"args\":[4]}"

# Get the current state
curl localhost:8080/games/tic-tac-toe/$MATCH | jq
```

For real-time updates, connect a WebSocket to
`ws://localhost:8080/games/tic-tac-toe/{matchID}/ws?playerID={id}`. The
server pushes:

- `sync` on connect (initial state + matchData)
- `update` after every accepted move
- `matchData` when the seated player list changes
- `chat` for chat messages
- `patch` (RFC 6902 JSON Patch) when `Game.DeltaState=true`
- `error` for rejected moves

Clients submit `{"type":"move","playerID":"...","credentials":"...","move":"...","args":[...],"stateID":N}` and
`{"type":"chat","payload":"..."}` over the same socket.

## Defining a game

```go
package main

import (
    "github.com/tjcran/boardgame-go/core"
    "github.com/tjcran/boardgame-go/match"
    "github.com/tjcran/boardgame-go/server"
    "github.com/tjcran/boardgame-go/storage"
)

type State struct{ Counter int }

func MyGame() *core.Game {
    return &core.Game{
        Name:       "counter",
        MinPlayers: 2,
        MaxPlayers: 2,
        Setup:      func(_ core.Ctx, _ any) core.G { return &State{} },
        Moves: map[string]any{
            "inc": core.MoveFn(func(mc *core.MoveContext, _ ...any) (core.G, error) {
                s := mc.G.(*State)
                return &State{Counter: s.Counter + 1}, nil
            }),
        },
        EndIf: func(mc *core.MoveContext) any {
            if mc.G.(*State).Counter >= 10 {
                return map[string]any{"winner": mc.Ctx.CurrentPlayer}
            }
            return nil
        },
        Turn: &core.TurnConfig{MinMoves: 1, MaxMoves: 1},
    }
}

func main() {
    m := match.NewManager(storage.NewMemory())
    m.MustRegister(MyGame())
    srv := server.New(m)
    srv.Origins = []string{server.OriginLocalhostInDevelopment}
    ctx := context.Background()
    _ = srv.Run(ctx, ":8080")
}
```

Prefer compile-time-typed G via [`typedgame`](typedgame/typedgame.go):

```go
import "github.com/tjcran/boardgame-go/typedgame"

var MyGame = typedgame.Game[*State]{
    Name: "counter",
    Setup: func(_ core.Ctx, _ any) *State { return &State{} },
    Moves: typedgame.Moves[*State]{
        "inc": typedgame.MoveFn[*State](func(mc *typedgame.Context[*State], _ ...any) (*State, error) {
            // mc.G is *State — no .(*State) needed.
            return &State{Counter: mc.G.Counter + 1}, nil
        }),
    },
}.Build()
```

For a fully wired tic-tac-toe see [`games/tictactoe`](games/tictactoe/).

## Wire protocol (REST)

Mirrors boardgame.io's Lobby API plus a few additions.

| Method | Path | Body | Returns |
|---|---|---|---|
| `GET` | `/games` | — | `["game-name", …]` |
| `GET` | `/games/{name}` | — | `{matches: [{matchID, players, ctx, …}]}` |
| `GET` | `/games/{name}/{id}` | — | full match summary |
| `GET` | `/games/{name}/byCode/{code}` | — | match by `JoinCode` |
| `POST` | `/games/{name}/create` | `{numPlayers, setupData, unlisted, name, joinCode}` | `{matchID}` |
| `POST` | `/games/{name}/{id}/join` | `{playerName, playerID, data}` | `{playerID, seat, playerCredentials}` |
| `POST` | `/games/{name}/{id}/leave` | `{playerID, credentials}` | `{}` |
| `POST` | `/games/{name}/{id}/update` | `{playerID, credentials, newName, data}` | `{}` |
| `POST` | `/games/{name}/{id}/playAgain` | `{playerID, credentials, numPlayers, setupData}` | `{nextMatchID}` |
| `POST` | `/games/{name}/{id}/move` | `{playerID, credentials, move, args, stateID, resumeTag}` | redacted state |
| `POST` | `/games/{name}/{id}/dryMove` | same as `/move` | would-be state (not persisted) |
| `POST` | `/games/{name}/{id}/reset` | `{playerID, credentials}` | `{}` |
| `GET` | `/games/{name}/{id}/ws?playerID={id}` | — (WebSocket) | frame stream |

Status codes: `200/201` on success, `400` for malformed JSON, `401` for
bad credentials, `404` for unknown game or match, `409` for rule
violations (wrong player, illegal move, seat taken, blocked cascade,
game over).

## Architecture

```
┌────────────────────────────────────────────────────────────────┐
│ cmd/boardgame-server  (thin main; signal-aware graceful shut) │
│ cmd/boardgame-go-vet  (go vet analyser for MoveFn determinism)│
└────────────────────────────────────────────────────────────────┘
                           │
┌────────────────────────────────────────────────────────────────┐
│ server/        REST handlers + WebSocket transport + CORS     │
│   ws.go        per-conn bounded send queue (drop-oldest)      │
│   admin.go     /debug/pprof + /debug/vars (opt-in)            │
└────────────────────────────────────────────────────────────────┘
                           │
┌────────────────────────────────────────────────────────────────┐
│ match.Manager  match lifecycle, per-match lock, broadcast     │
│                lifecycle hooks, OnLifecycle subscribers       │
│                turn timers, janitor, OCC retry loop           │
│                schema migration on load                       │
└────────────────────────────────────────────────────────────────┘
       │                  │                  │
┌──────────────┐  ┌────────────────┐  ┌──────────────────────┐
│ storage.*    │  │ core/  (pure)  │  │ plugins/             │
│   Memory     │  │  Game, State   │  │   random  (seeded)   │
│   FlatFile   │  │  Apply / Queue │  │   player  (per-seat) │
│   SQLite     │  │  Replay        │  └──────────────────────┘
│   Postgres   │  │  Plugin lifecycle ↑                       │
│   Redis      │  └────────────────┘
│   Cached     │
│   WriteBehind│
│   Invalidator│
└──────────────┘
                    │
   ┌──────────────────────────────┐  ┌─────────────────────────────┐
   │ bots/                        │  │ modules/   (all opt-in)     │
   │   RandomBot                  │  │   ccg/       entities/zones │
   │   MCTSBot (Perspective)      │  │              modifiers/bus  │
   │   AutoPlayer                 │  │   tabletop/  Board, LOS,    │
   │   Simulate                   │  │              dice, Resolve  │
   │   llm/  (OpenAI / OpenRouter │  │   economy/   per-turn pools │
   │          / Anthropic, tools) │  │   shop/      market + Buy   │
   └──────────────────────────────┘  └─────────────────────────────┘
```

The `core` package has no I/O dependencies. The engine never imports
anything under `modules/`, `bots`, `plugins`, `server`, `match`, or
any storage backend.

## Features

Every item below is shipped and test-covered. See [PARITY.md](PARITY.md)
for the full boardgame.io feature checklist.

### Engine

- Phases, stages, active-players (with `ALL`/`OTHERS`/`Once` presets)
- Turn-order strategies (Default / Reset / Continue / Once / Custom /
  CustomFrom) and ad-hoc `endTurn({next})`
- Hooks: phase OnBegin/OnEnd/EndIf, turn OnBegin/OnEnd/OnMove/EndIf,
  stage OnBegin/OnEnd, game OnEnd, OnUndo intercept
- Events from inside moves: `EndTurn`/`Pass`/`EndPhase`/`SetPhase`/
  `EndStage`/`SetStage`/`EndGame`/`SetActivePlayers`/`RemovePlayer`
- Action queue / cascade primitive with pause/resume blocks (BlockSpec
  + ResumeTag matching). MTG-style trigger stacks for free.
- Undo / Redo with per-turn snapshots, `Game.DisableUndo`,
  per-move `Undoable`, log redaction via per-move `Redact`
- Pure replay: `core.Replay` / `core.ReplayUntil(n)` reproduces a
  match deterministically given the seeded PRNG
- `context.Context` propagated through `Apply` for per-move
  cancellation and `Move.Timeout`
- Compile-time-typed games via [`typedgame`](typedgame/)
- `Game.Validate()` catches dangling `Phase.Next`, duplicate `Start`,
  malformed move tables, `MinPlayers > MaxPlayers` at registration

### Plugin lifecycle

- `Plugin` interface with `Setup` / `API` / `Flush` / `IsInvalid` /
  `PlayerView` / `FnWrap` hooks
- Seeded RNG ([`plugins/random`](plugins/random/)) — `Die`, `D4–D20`,
  `Number`, generic `Shuffle[T]`. State stripped from `PlayerView`.
- Per-seat records ([`plugins/player`](plugins/player/)) — BGIO's
  `PluginPlayer` with `Opponent` helper

### Server / Lobby

- Full BGIO REST surface above
- Per-seat credentials (16-byte hex by default), pluggable
  `GenerateCredentials` / `AuthenticateCredentials`
- `Game.AllowDynamicPlayers` for mid-match join (BGIO can't)
- `Game.SpectatorsAllowed` for locking matches to seated players
- Optional optimistic-concurrency mode for multi-instance deployments
  via `Manager.UseOptimisticConcurrency` + `storage.OptimisticStorage`
- BGIO-style WS frames: `sync` / `update` / `matchData` / `chat` /
  `patch` / `error`
- DeltaState transport via RFC 6902 JSON Patch when
  `Game.DeltaState=true`
- StateID-based stale-move guard (`Manager.RequireStateID`)
- CORS with `Origins.LOCALHOST_IN_DEVELOPMENT` / `LOCALHOST` presets
- Body-size limit via `Server.MaxBodyBytes` (8 MiB default)
- Heartbeat ping/pong every 25s + `IsConnected` flips broadcast on
  socket open/close
- Match janitor (`Manager.RunJanitor`) wipes idle matches
- Turn timer (`Turn.TimeBudget` + auto-expire goroutine)
- Lifecycle subscriptions (`Manager.OnLifecycle` /
  `OnLifecycleKind`) for `match.created` / `joined` / `left` / `moved`
  / `gameOver` / `reset`
- `Manager.ExportMatch` / `ImportMatch` for portable replay bundles
- `Manager.Reset` + `Manager.DryMove` for client.reset and move preview
- Schema versioning + migration on load (`Game.SchemaVersion` +
  `Game.Migrate`)

### Bots

Built-ins ([`bots/`](bots/bots.go)):

- `RandomBot` and `MCTSBot` honour `Game.Enumerate`
- `MCTSBot.Perspective` runs rollouts against `core.PlayerView` so
  AIs can't read opponent secret state
- `MCTSBot.EarlyStop` cuts the search short when one branch dominates
- `AutoPlayer` runs a bot against a live match as a Subscriber
- `bots.Simulate` runs N matches between bots and reports aggregate
  stats (wins, draws, errors, avg moves, avg turns)

LLM-backed bots ([`bots/llm/`](bots/llm/)):

- Provider-agnostic `Provider` interface; thin HTTP clients, no vendor
  SDKs
- `NewOpenAIProvider` — also serves OpenRouter / Together / Groq /
  Fireworks via `BaseURL` override. `NewOpenRouterProvider` is a
  one-liner convenience
- `NewAnthropicProvider` — translates to Anthropic's Messages API
  (top-level system, content-array responses, `input_schema`)
- **Tool-calling first.** Each enumerated `Action` becomes one
  no-parameter tool; LLM picks one by name; engine parses the choice
  back into a `bots.Action`. No regex parsing of LLM prose; no
  injection vector for action choice.
- `ToolModeRequired` (default) forces a structured choice;
  `ToolModeAuto` lets the model reason in text first; `ToolModeFree`
  with a `ParseFreeText` callback for custom flows
- Hook-based prompting: required `PromptFn(state, playerID, actions)`
  returns `(system, user)`. `llm.DefaultPrompt` is a starting helper.
- `MockProvider` + `MockPickAction` for tests; no real API calls in
  the suite
- Temperature defaults to 0; `MaxRetries` / `RetryFn` hooks; honours
  `ctx` cancellation
- **Not replay-safe.** Model versions and provider batching make
  byte-identical replay impossible — use `RandomBot` / `MCTSBot` for
  deterministic replay

```go
prov := llm.NewAnthropicProvider(llm.AnthropicOpts{APIKey: os.Getenv("ANTHROPIC_API_KEY")})
bot := &llm.LLMBot{
    Provider: prov,
    Model:    "claude-opus-4-7",
    Game:     myGame,
    PromptFn: myDomainPrompt,  // render state + cards + threats for the model
    Describe: myActionDescriber,  // "Play Pilfer targeting opponent's 4/3"
}
auto := &bots.AutoPlayer{Bot: bot, Manager: m, MatchID: id,
    PlayerID: "1", Credentials: cr}
go auto.Run(ctx)
```

Composition: write your own `bots.Bot` to chain bots — e.g. MCTS for
candidate generation + LLM for selection.

### Storage backends

| Backend | When to use |
|---|---|
| `storage.NewMemory()` | Dev, tests, single-process |
| `storage.NewFlatFile(dir)` | Single-process with disk durability |
| `storage/sqlite` | Embedded persistence, pure Go (no CGO) |
| `storage/postgres` | Multi-process shared state |
| `storage/redis` | Fast shared state, small footprint per match |
| `storage.Cached` (decorator) | In-memory TTL+LRU cache wrapping any storage |
| `storage.WriteBehind` (decorator) | Async batched writes with per-match coalescing |
| `storage/redis.Invalidator` | Cross-node cache-bust pubsub for Cached |

### Determinism analyser

`cmd/boardgame-go-vet` is a `go vet`-style analyser that flags
non-deterministic calls (`time.Now`, `math/rand.*`, `crypto/rand.Read`,
`os.Getenv`, etc.) inside any `MoveFn` / `HookFn` / `EndIfFn` body.
Install and run:

```bash
go install github.com/tjcran/boardgame-go/cmd/boardgame-go-vet@latest
go vet -vettool=$(which boardgame-go-vet) ./...
```

### CCG / TCG library

[`modules/ccg/`](modules/ccg/) — entities, zones, layered modifiers, sync event bus,
target queries, plus an optional `Catalog` of `CardDef` templates so
games can stamp instances with `State.Instantiate(catalog, defID, owner)`,
a `DeckList` + composable validators (`MinSize` / `MaxSize` /
`MaxCopies` / `RequireDefsExist` / `NonNegativeCounts` / `Compose`)
materialised into a zone via `State.LoadDeckList` in deterministic
DefID order, and per-entity `Visibility` (`Public` / `OwnerOnly` /
`Hidden`) plus `State.RedactForViewer(viewer)` that returns a deep-copied
state with hidden entities, their modifiers, and events that reference
them stripped — drop it into your game's `PlayerView` instead of
hand-rolling the strip-opponent-hand dance, and a typed counter API
(`AddCounter` / `RemoveCounter` / `Counters` / `AllCounters` /
`TransferCounters`) that publishes a `counter_changed` event with
`{kind, delta, total_after}` on every mutation so triggered abilities
can subscribe, and one-shot helpers `Transition` / `MoveAllTo` (pre-
flight all-or-nothing batch moves), `Top` / `Bottom`, `Mulligan`
(empty hand → shuffle deck → redraw), `Deal` (per-player × per-def
opening-hand setup), a `DeckPile{DeckZone, DiscardZone}` composite
with `Draw` / `Discard` / `Mill` / `ReshuffleDiscardIntoDeck` methods
and an opt-in `AutoReshuffleOnEmpty` policy (default off, so MTG-
style "empty deck loses" stays as the un-opinionated default), and
`BindAbility(id, zones, predicate, handler)` — lifecycle-managed
event subscription that auto-unbinds when the entity leaves the
listed zones or is destroyed, closing the classic "creature still
triggers from the graveyard because someone forgot Unsubscribe"
bug. The optional [`modules/ccg/typed`](modules/ccg/typed/) sub-package adds generic
`CardDef[A]` / `Catalog[A]` so games can author card templates whose
Attrs is a typed struct; the underlying untyped `ccg.Catalog` is
reachable via `cat.Untyped()` and flows through `State.Instantiate`
/ `LoadDeckList` unchanged. Bookkeeping only; no game semantics.
Composes with the action queue + Random + Replay. Engine never
imports it; importers pay nothing if their game isn't card-shaped.

### Wargame library

[`modules/tabletop/`](modules/tabletop/) — spatial + dice primitives for
miniatures wargames (40k-shaped, Battletech-shaped, Necromunda-shaped).
A `Board` interface (`InBounds` / `Distance` / `Neighbors` / `Line`)
with two concrete impls: `SquareBoard` (Chebyshev distance, 8-way
neighbors, Bresenham line) and `HexBoard` (axial coordinates, hex
distance, cube-coord-round line whose length equals `Distance + 1`).
LOS is a board-agnostic free function — `LineOfSight(board, from, to,
blocks)` walks `Line` and asks the blocker predicate about every
intermediate cell. A `TerrainMap` provides sparse per-cell tag storage
(`Tag` / `Untag` / `HasTag` / `Blocks` sugar for the LOS-blocking
case). `tabletop.State` tracks `UnitID → Pos` mappings with a lazy
reverse cell→units index built on first access (rebuilds correctly
after JSON unmarshal), plus `Place` / `Move` / `Remove` / `PositionOf`
/ `EntitiesAt` / `Within(board, center, radius)` queries. Dice helpers
include `Pool{Dice, Sides}.Roll(*core.Random)`, `Successes(rolls,
target)`, `RerollBelow(rolls, threshold, *core.Random)`, and the
40k-shaped `Resolve{Attacks, HitOn, WoundOn, SaveOn}.Run(*core.Random)`
hit-wound-save chain that returns `ResolveResult{Hits, Wounds, Unsaved}`.
Units are identified by an opaque `tabletop.UnitID = uint64` so games
also using `ccg/` can pass `tabletop.UnitID(ccgEntity.ID)` — same
numeric space, zero coupling. Engine never imports it; importers pay
nothing if their game isn't spatial.

### Turn-economy library

[`modules/economy/`](modules/economy/) — per-turn resource pools (gold,
actions, buys) for deckbuilder-shape and auto-battler-shape games.
`Pool{Owner, Kind, Cap}` is a thin config struct over `ccg.Counters`:
storage is just a named counter on a player entity. What economy adds
on top is currency semantics — `Cap` enforcement on `Gain` and `Set`
(clamps to the configured max, returns the applied delta), and
`Spend(s, n)` that returns `ErrInsufficient` and leaves state untouched
when the pool has less than `n` (ccg's `RemoveCounter` would silently
clamp at 0 — that's a bug for currency). `Scaled(turn, base, per, max)`
is the canonical "base + (turn − 1) × per, capped at max" income curve
— drop it straight into `Pool.Set` at turn start. Every operation
routes through `ccg.Counters` so the `counter_changed` event fires
under the hood; card abilities that listen for "gold gained" or
"gold spent" Just Work. Engine never imports it.

### Shop library

[`modules/shop/`](modules/shop/) — refreshable market for auto-battler
and deckbuilder games. `Shop{Slots, Stock, Size}` is two `ccg.Zone`
names plus a target size. Operations: `Clear(s, dest)` removes
un-frozen items from `Slots` (destroying them when `dest == ""`,
or moving them to `dest` for shared-pool semantics); `Fill(s, r)`
draws from `Stock` up to `Size`, stopping early if `Stock` runs
dry; `Roll(s, r, dest)` is sugar for `Clear` + `Fill` — the typical
"spend 1 gold to reroll" move. `Freeze` / `Unfreeze` / `IsFrozen`
mark a slot entry to survive the next `Roll` (stored as a reserved
`Entity.Attrs[ShopFrozenAttrKey]` bool). `Buy(s, item, destination)`
moves the item to a destination zone and clears the freeze flag so
shop state doesn't leak into the player's hand. `Shop` does NOT enforce
cost — games compose `economy.Pool.Spend` and `shop.Buy` in their move
handlers (the doc comment shows the pattern). Engine never imports it.

## Comparison vs boardgame.io

### Parity — features both frameworks ship

| Area | boardgame.io | boardgame-go |
|---|:---:|:---:|
| Declarative `Game` with `Setup` / `Moves` / `EndIf` / `OnEnd` | ✅ | ✅ |
| Phases, stages, active-players (with `ALL`/`OTHERS`/`Once` presets) | ✅ | ✅ |
| Turn-order strategies (Default/Reset/Continue/Once/Custom/CustomFrom) | ✅ | ✅ |
| Phase / Turn `OnBegin` / `OnEnd` / `OnMove` / `EndIf` hooks | ✅ | ✅ |
| Events from inside moves (`endTurn` / `setPhase` / `setStage` / `endGame` / …) | ✅ | ✅ |
| Undo / Redo with per-turn snapshots and `DisableUndo` | ✅ | ✅ |
| Move log with per-move `Redact` redaction | ✅ | ✅ |
| Seeded RNG plugin (`Die` / `D4`–`D20` / `Shuffle`) | ✅ | ✅ |
| Plugin lifecycle (`Setup` / `API` / `Flush` / `IsInvalid` / `PlayerView` / `fnWrap`) | ✅ | ✅ |
| `PluginPlayer` (per-seat records with Opponent helper) | ✅ | ✅ |
| Secret state via `PlayerView` (+ `STRIP_SECRETS` helper) | ✅ | ✅ |
| Full Lobby REST API (create/join/leave/update/playAgain/move) | ✅ | ✅ |
| Per-seat credentials with pluggable generate/authenticate hooks | ✅ | ✅ |
| WebSocket transport with `sync` / `update` / `matchData` / `chat` frames | ✅ | ✅ |
| JSON-Patch `deltaState` transport | ✅ | ✅ |
| CORS with `LOCALHOST_IN_DEVELOPMENT` preset | ✅ | ✅ |
| FlatFile storage adapter | ✅ | ✅ |
| MCTS + Random bots | ✅ | ✅ |

### Beyond BGIO — features we ship that BGIO doesn't

| Feature | Why it matters | BGIO equivalent |
|---|---|---|
| **Cross-match concurrency by default** (goroutines) | Manager flat at ~3 µs/op across 1–64 concurrent matches | Single Node event loop bottlenecks everything |
| **Action queue + drain primitive** (`mc.Queue.Push` / `Block` with `ResumeTag`) | MTG-style trigger cascades with pause/resume as first-class engine state | None — users hand-roll `processNext` moves |
| **`modules/ccg/` library** (entities, zones, layered modifiers, event bus, target queries) | CCG / TCG / deckbuilder bookkeeping with no opinionated semantics | None |
| **`modules/tabletop/` library** (Board interface w/ Square + Hex impls, positions w/ reverse index, terrain tags, LOS, dice pools, hit-wound-save Resolve) | Wargame-shape spatial + dice primitives, composes with `modules/ccg/` for unit stats | None |
| **`modules/economy/` library** (Pool with cap, Gain/Spend/Set, Scaled income helper) | Deckbuilder / auto-battler turn-economy on top of `ccg.Counters` — currency semantics ccg's "clamps at 0" floor wouldn't give you | None |
| **`modules/shop/` library** (Shop with Slots/Stock/Size, Freeze, Clear/Fill/Roll, Buy) | Refreshable market for shop-phase games; composes cleanly with `modules/economy/` for cost-paying | None |
| **Compile-time-typed games** (`typedgame.Game[S]`) | `mc.G.Score` with no runtime asserts | TypeScript types help IDE; framework is still untyped at runtime |
| **`Move.Timeout`** | Cooperative per-move cancellation via `context.Context` | None |
| **`Move.IgnoreBlocks`** | Concede / forfeit can bypass cascade pause | None |
| **`MCTSBot.Perspective`** | AI can't read opponent secret state during rollouts | MCTS sees full state; effectively cheats |
| **`MCTSBot.EarlyStop`** | Halts search when one branch dominates | None |
| **`bots/llm/`** | OpenAI / OpenRouter / Anthropic with tool-calling-first design | None |
| **`bots.Simulate`** | N-match round-robin between bots with aggregate stats | None |
| **`Manager.OnLifecycle`** | Subscribe to `match.created` / `joined` / `left` / `moved` / `gameOver` / `reset` | Ad-hoc broadcast hook lists |
| **`Manager.AutoExpire` + `Turn.TimeBudget`** | Engine-driven turn timers | Open since 2017 (BGIO #92) |
| **`Manager.RunJanitor`** | Background sweep wipes idle matches | None |
| **`Manager.DryMove`** | Preview a move without persisting (UI hover) | Open since 2019 (BGIO #636) |
| **`Manager.Reset`** | Reset multiplayer match keeping seats + credentials | Open (BGIO #1166) |
| **`Manager.ExportMatch` / `ImportMatch`** | Portable bundle for replay / debugging / AI training | None |
| **`Game.AllowDynamicPlayers`** | Mid-match join expanding PlayOrder + NumPlayers | None (BGIO #884, #1102) |
| **`Game.SpectatorsAllowed`** | Lock matches to seated players | None (BGIO #1007) |
| **`Game.SchemaVersion` + `Migrate`** | Schema evolution for long-lived persisted matches | None |
| **`Game.Validate()` at registration** | Catches dangling `Phase.Next`, duplicate `Start`, malformed moves before runtime | None |
| **`Game.Enumerate`** | First-class legal-action function shared by all bots | BGIO #1078 — only used internally by AI |
| **`Events.RemovePlayer` / `RunMove`** | Player elimination, chained move dispatch | BGIO #616, #1085 |
| **Stage `OnBegin` / `OnEnd` hooks** | Pre/post-entry hooks for intra-turn sub-states | BGIO #608 (docs say they exist; never landed) |
| **`mc.AddLog` from hooks** | Custom log entries from inside `OnBegin` / `OnEnd` | BGIO #1228 (log plugin only saw moves) |
| **`Game.OnUndo`** | Scrub transient fields (animations) on undo | BGIO #1135 |
| **Match join codes** | Short invite codes (e.g. `ABC123`) instead of opaque IDs | BGIO #574 (WIP, never landed) |
| **Heartbeat + `IsConnected` tracking** | 25 s ping/pong flips connected flag; matchData broadcast | None |
| **Per-WS bounded send queue** | Slow client can't block the per-match write lock | Open bug — Node serialises subscriber writes |
| **Deterministic replay** (`core.Replay` / `ReplayUntil`) | Re-apply a recorded log to byte-identical final state | Leaks `Date.now`/`Math.random`; not reliable |
| **`cmd/boardgame-go-vet`** | `go vet` analyser flagging `time.Now`/`math/rand` in moves | Documented contract; not enforced |
| **`context.Context` propagation** | Cancellation + deadlines through `Apply` | None |
| **`Manager.UseOptimisticConcurrency`** | Multi-instance deployments without sticky sessions | Requires sticky LB + Redis adapter |
| **5 storage backends** (Memory / FlatFile / **SQLite** / **Postgres** / **Redis**) | Pick durability + scale tier per deployment | Memory + FlatFile in-tree; community packages for the rest |
| **3 storage decorators** (`Cached` / `WriteBehind` / `Invalidator`) | TTL+LRU cache, async batched flushes, cross-node cache busts | None |
| **`/debug/pprof` + `/debug/vars`** admin mux | Free profiling + per-event counters | Add via npm packages |
| **Structured `log/slog`** | `match_id` / `player_id` / `move` / `dur_us` fields | `console.log` |
| **Single static binary** | Cross-compile to any platform; no Node runtime needed | Node + npm install pipeline |

### Not in this repo — BGIO ships, we skip

| Feature | Why we skipped |
|---|---|
| React `<Client/>` / `<Lobby/>` components | Intrinsically JS + DOM; no Go analogue |
| Redux store / enhancer / DevTools integration | JS-only |
| In-browser Debug Panel | JS-only |
| Plain JS `Client` / `LobbyClient` | The wire protocol is language-agnostic JSON; any client works |
| TypeScript transport library | Filed as [issue #6](https://github.com/tjcran/boardgame-go/issues/6) — cross-language release process is the open question |

## Performance

From `bench/` on a Ryzen 9 5900X:

```
BenchmarkSingleMatchSerialMoves                       ~615 ns/op   3 allocs/op
BenchmarkConcurrentMatchesViaManager/concurrency=1   ~3.0 μs/op  17 allocs/op
BenchmarkConcurrentMatchesViaManager/concurrency=4   ~3.0 μs/op  17 allocs/op
BenchmarkConcurrentMatchesViaManager/concurrency=16  ~3.0 μs/op  17 allocs/op
BenchmarkConcurrentMatchesViaManager/concurrency=64  ~3.0 μs/op  17 allocs/op
```

The flat scaling across 1–64 concurrent matches is the headline: Go's
goroutines give us actual cross-match parallelism. BGIO can't match it
on Node's single event loop. See [`bench/README.md`](bench/README.md)
for caveats and how to run.

## What's deliberately not in this repo

- React `<Client/>` / `<Lobby/>` components — intrinsically JS / DOM,
  no Go analogue
- Redux store + DevTools integration — JS-only
- In-browser Debug Panel — JS-only
- BGIO's `LobbyClient` JS wrapper — JS-only

A standalone TS transport library is filed as
[#6](https://github.com/tjcran/boardgame-go/issues/6) with the
cross-language maintenance trade-off; revisit when there's concrete
demand.

## Development

```bash
go test ./... -race          # 18 packages
go vet ./...
go build ./...
go test -bench . ./bench/    # cross-match concurrency numbers
```

## Layout

```
core/         pure game engine — no I/O, no concurrency primitives
                Game, State, Ctx, Move(Context), Events, Queue,
                Turn/Phase/Stage, Plugin interface, Replay, Undo
modules/ccg/  (opt-in) CCG-shape bookkeeping: entities, zones,
                modifiers (layered), event bus, target queries
modules/tabletop/ (opt-in) Wargame-shape spatial + dice primitives:
                Board (Square/Hex), positions, terrain, LOS,
                dice pools, hit-wound-save Resolve chain
modules/economy/ (opt-in) Per-turn resource pools (gold, actions,
                buys) with cap + Spend(ErrInsufficient), built on
                ccg.Counters
modules/shop/ (opt-in) Refreshable market with Freeze, Roll, Buy;
                composes with modules/economy/ for cost-paying
                purchases
typedgame/    (opt-in) Generics wrapper over core for typed G
match/        Manager: lifecycle, locks, broadcast, lifecycle hooks,
                turn timers, janitor, OCC retry, schema migration
storage/      interface + Memory, FlatFile, Cached, WriteBehind
storage/sqlite     pure-Go SQLite (modernc.org/sqlite, no CGO)
storage/postgres   pgx-via-database/sql; OptimisticStorage capable
storage/redis      go-redis; Invalidator for cross-node cache busts
server/       HTTP REST + WebSocket transport + CORS + admin mux
plugins/random     seeded splitmix64 PRNG
plugins/player     BGIO's PluginPlayer (per-seat records)
bots/         Bot interface, RandomBot, MCTSBot, AutoPlayer, Simulate
bots/llm/     LLM-backed bots (OpenAI / OpenRouter / Anthropic),
                tool-calling-first; thin HTTP, no vendor SDKs
testhelpers/  Scenario builder + MockRandom
bench/        Cross-match concurrency benchmarks + README
cmd/boardgame-server     reference binary
cmd/boardgame-go-vet     determinism analyser
games/tictactoe          reference game
mcp/                     (opt-in sub-module) MCP server exposing
                           matches to Claude Desktop / Claude Code /
                           Cursor / any MCP-capable LLM client
```

## Play against an LLM

The `mcp/` sub-module ships a Model Context Protocol server that
exposes any registered game to LLM clients. Local play (Claude Desktop,
Claude Code, Cursor): grab a release binary or `docker pull`, drop a
snippet into the client's config, and Claude becomes your opponent.

See [mcp/README.md](mcp/README.md) for the install paths and
copy-pasteable Claude Desktop / Claude Code configs.

This is **opt-in**: `mcp/` is its own Go sub-module, so the root
engine's dependency graph is unchanged for library consumers who don't
want it (`go build ./...` from the repo root never pulls MCP code or
its deps — enforced by a CI job).

## Related docs

- [PARITY.md](PARITY.md) — boardgame.io feature checklist
- [GO_LEVERAGE.md](GO_LEVERAGE.md) — wins from Go's type system / stdlib
- [JS_LIMITATIONS.md](JS_LIMITATIONS.md) — flaws in BGIO Go solves
- [PLAN.md](PLAN.md) — original MVP plan (historical)
- [mcp/README.md](mcp/README.md) — MCP server (LLM gameplay)
- [mcp/docs/local-mcp.md](mcp/docs/local-mcp.md) — local Claude Desktop / Code setup
- [mcp/docs/hosted-app.md](mcp/docs/hosted-app.md) — hosted Claude.ai connector deploy

## Acknowledgments

The public API surface — `Game`, `Setup`, `Moves`, `Phases`, `Turn`,
`EndIf`, the Lobby contract, the plugin interface — deliberately mirrors
[boardgame.io](https://github.com/boardgameio/boardgame.io) so that game
logic translates between the two frameworks with mechanical changes.
boardgame.io is the original work of Nicolo Davis and the wider BGIO
contributor community; this Go port stands on top of years of their
design thinking. No upstream source is included in this repo — the Go
implementation is freshly authored — but the upstream MIT license is
reproduced verbatim in [NOTICE](NOTICE) as a courtesy and to remove any
ambiguity for downstream users.

## License

MIT — see [LICENSE](LICENSE). Upstream attribution lives in
[NOTICE](NOTICE).
