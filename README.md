# boardgame-go

A Go-based engine and authoritative server for turn-based board games,
inspired by [boardgame.io](https://github.com/boardgameio/boardgame.io).

> **Status:** MVP. Core engine, in-memory match manager, HTTP REST + WebSocket
> transport, and a tic-tac-toe reference game. See [PLAN.md](PLAN.md) for the
> roadmap and what's intentionally deferred.

## Why

boardgame.io is a brilliant framework for prototyping board games, but it's
JavaScript-only. If you want to host games from a Go service — alongside
existing APIs, behind your own auth, deployed as a small static binary — this
project is a Go-native take on the same ideas:

- **Declarative game definitions.** A `Game` is data: an initial state, a
  table of `Moves`, optional `EndIf`. The framework is the reducer.
- **Authoritative server.** The server owns canonical state. Clients submit
  moves; the server validates, runs the reducer, advances the turn, and
  pushes the new state.
- **Pure-ish reducers.** Moves are `func(G, Ctx, args...) G`. Deterministic.
  Easy to unit-test without spinning up a server.
- **Transport-agnostic core.** REST and WebSockets are thin shells around
  the same `match.Manager`.

## Quick start

```bash
# Install Go 1.22+, then:
go run ./cmd/boardgame-server -addr :8080
```

In another shell:

```bash
# Create a match
MATCH=$(curl -s -X POST localhost:8080/games/tic-tac-toe/create -d '{}' \
  | jq -r .matchID)

# Seat two players
ALICE=$(curl -s -X POST localhost:8080/games/tic-tac-toe/$MATCH/join \
  -d '{"name":"alice"}' | jq -r .playerID)
BOB=$(curl -s -X POST localhost:8080/games/tic-tac-toe/$MATCH/join \
  -d '{"name":"bob"}' | jq -r .playerID)

# Alice plays cell 4 (center)
curl -X POST localhost:8080/games/tic-tac-toe/$MATCH/move \
  -d "{\"playerID\":\"$ALICE\",\"move\":\"clickCell\",\"args\":[4]}"

# Get the current state
curl localhost:8080/games/tic-tac-toe/$MATCH/state | jq
```

For real-time updates, connect a WebSocket to
`ws://localhost:8080/games/tic-tac-toe/{matchID}/ws`. The server pushes
`{"type":"state","state":{...}}` after every accepted move. Send
`{"type":"move","playerID":"...","move":"...","args":[...]}` to submit moves.

## Defining a game

Drop a function somewhere that returns a `*core.Game`:

```go
import "github.com/tjcran/boardgame-go/core"

type State struct{ Counter int }

func MyGame() *core.Game {
    return &core.Game{
        Name:       "counter",
        MinPlayers: 2,
        MaxPlayers: 2,
        Setup:      func(_ int) core.G { return &State{} },
        Moves: map[string]core.MoveFn{
            "increment": func(g core.G, _ core.Ctx, _ ...any) (core.G, error) {
                s := g.(*State)
                return &State{Counter: s.Counter + 1}, nil
            },
        },
        EndIf: func(g core.G, ctx core.Ctx) (bool, string, bool) {
            if g.(*State).Counter >= 10 {
                return true, ctx.CurrentPlayer, false
            }
            return false, "", false
        },
    }
}
```

Then register and serve:

```go
m := match.NewManager(storage.NewMemory())
m.Register(MyGame())
http.ListenAndServe(":8080", server.New(m))
```

See [`games/tictactoe`](games/tictactoe/tictactoe.go) for a fully wired
example.

## Wire protocol

REST (JSON, all responses are JSON):

| Method | Path                                       | Body                                | Returns                                  |
| ------ | ------------------------------------------ | ----------------------------------- | ---------------------------------------- |
| POST   | `/games/{game}/create`                     | `{numPlayers?}`                     | `{matchID}`                              |
| GET    | `/games/{game}/matches`                    | —                                   | `[{id, gameName, players, ctx}]`         |
| POST   | `/games/{game}/{matchID}/join`             | `{playerID?, name, seat?}`          | `{playerID, seat}`                       |
| GET    | `/games/{game}/{matchID}/state`            | —                                   | full match record                        |
| POST   | `/games/{game}/{matchID}/move`             | `{playerID, move, args}`            | new `State` or error                     |
| GET    | `/games/{game}/{matchID}/ws`               | — (WebSocket)                       | streams `{type:"state", state}` frames   |

Status codes:

- `404` — unknown game or unknown match
- `409` — rule violation (wrong player, illegal move, seat taken, game over)
- `400` — malformed JSON
- `200/201` — success

## Architecture

```
       ┌──────────────────────────────────────────────┐
       │  cmd/boardgame-server     (thin main)        │
       └──────────────────────────────────────────────┘
                            │
       ┌──────────────────────────────────────────────┐
       │  server/                                     │
       │   http.go ─── REST handlers                  │
       │   ws.go   ─── WebSocket transport            │
       └──────────────────────────────────────────────┘
                            │
       ┌──────────────────────────────────────────────┐
       │  match.Manager                               │
       │   - registers games                          │
       │   - per-match write lock                     │
       │   - broadcasts to subscribers                │
       └──────────────────────────────────────────────┘
              │                              │
   ┌────────────────────┐         ┌────────────────────┐
   │  storage.Storage   │         │  core (pure)       │
   │   memory / SQL /…  │         │   Game, State,     │
   │                    │         │   Apply(reducer)   │
   └────────────────────┘         └────────────────────┘
```

The core package has no dependencies and no I/O — that's what lets you unit
test rules in microseconds and replay a transcript deterministically (once we
add a log).

## What's missing vs. boardgame.io

The big stuff intentionally deferred from MVP (PRs welcome):

- **Phases / stages / activePlayers** — multi-phase turns are central in
  boardgame.io. The MVP supports a single linear turn order.
- **Secret state (`playerView`)** — per-seat redaction of `G` before sending
  to clients.
- **Random number plugin** — seeded, replayable RNG accessible from moves.
- **Undo / redo / replay log** — record the move list, support time travel.
- **Bots / AI** — random + MCTS bots that play against the same `Game`
  definition.
- **Persistent storage** — Postgres / SQLite implementations of
  `storage.Storage`.
- **Authentication and per-seat credentials.**
- **React client bindings** (or any client bindings). The wire protocol is
  language-agnostic JSON, so any client works.

## Development

```bash
go test ./...      # all packages
go vet ./...
go build ./...
```

## Layout

```
core/         pure game engine — no I/O, no concurrency primitives
match/        match lifecycle wraps the engine
storage/      persistence interface + memory implementation
server/       HTTP REST + WebSocket
games/        reference games (tic-tac-toe)
cmd/          binaries
```
