# boardgame-go

A Go-based engine and authoritative server for turn-based board games,
card games, deckbuilders, and engine-builders. Inspired by
[boardgame.io](https://github.com/boardgameio/boardgame.io); structurally
a superset of its non-React surface, plus the features Go's runtime and
type system make easy that JavaScript's don't.

> **Status:** production-shaped. Full BGIO parity for the engine,
> plugins, and lobby; 22+ open BGIO issues addressed (closed in our
> port); CCG-shape primitives library on top. See
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
   ┌────────────────────────┐  ┌─────────────────┐
   │ bots/                  │  │ ccg/  (opt-in)  │
   │   RandomBot            │  │  entities       │
   │   MCTSBot (Perspective)│  │  zones          │
   │   AutoPlayer           │  │  modifiers      │
   │   Simulate             │  │  event bus      │
   └────────────────────────┘  │  target queries │
                               └─────────────────┘
```

The `core` package has no I/O dependencies. The engine never imports
`bots`, `ccg`, `plugins`, `server`, `match`, or any storage backend.

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

- `RandomBot` and `MCTSBot` honour `Game.Enumerate`
- `MCTSBot.Perspective` runs rollouts against `core.PlayerView` so
  AIs can't read opponent secret state
- `MCTSBot.EarlyStop` cuts the search short when one branch dominates
- `AutoPlayer` runs a bot against a live match as a Subscriber
- `bots.Simulate` runs N matches between bots and reports aggregate
  stats (wins, draws, errors, avg moves, avg turns)

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

[`ccg/`](ccg/) — entities, zones, layered modifiers, sync event bus,
target queries. Bookkeeping only; no game semantics. Composes with the
action queue + Random + Replay. Engine never imports it; importers pay
nothing if their game isn't card-shaped.

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
ccg/          (opt-in) CCG-shape bookkeeping: entities, zones,
                modifiers (layered), event bus, target queries
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
testhelpers/  Scenario builder + MockRandom
bench/        Cross-match concurrency benchmarks + README
cmd/boardgame-server     reference binary
cmd/boardgame-go-vet     determinism analyser
games/tictactoe          reference game
```

## Related docs

- [PARITY.md](PARITY.md) — boardgame.io feature checklist
- [GO_LEVERAGE.md](GO_LEVERAGE.md) — wins from Go's type system / stdlib
- [JS_LIMITATIONS.md](JS_LIMITATIONS.md) — flaws in BGIO Go solves
- [PLAN.md](PLAN.md) — original MVP plan (historical)
