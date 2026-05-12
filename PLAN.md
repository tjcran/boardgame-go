# boardgame-go MVP Plan

A Go-based port / alternative to [boardgame.io](https://github.com/boardgameio/boardgame.io).

## Project name

Working name: `boardgame-go`. Module path: `github.com/tjcran/boardgame-go`
(can be moved later — picked now so imports compile).

## Design goals carried over from boardgame.io

1. **Declarative game definitions.** A `Game` is data: an initial state, a
   `Moves` table, an optional turn order, optional `Phases`, an `EndIf`. The
   framework is the reducer.
2. **Authoritative server.** The server owns canonical state. Clients submit
   moves; the server validates, runs the reducer, increments a turn, and pushes
   the new state.
3. **Pure-ish reducers.** A move is `func(G, Ctx, args...) G` (or returns
   `INVALID_MOVE`). It must be deterministic given the inputs we feed it —
   that's what makes replay, multiplayer, and log/undo possible later.
4. **Transport-agnostic core.** The reducer + match state doesn't know about
   HTTP or WebSockets. A `Server` wraps it; transports plug in.

## In-scope for MVP (this session)

- Core types: `Game`, `State` (G + Ctx), `Move`, `Ctx`, `MoveResult`.
- Reducer: `Apply(state, move) -> (state, error)` with validation + turn
  advancement.
- Turn management: simple linear turn order over N players + `EndIf` for
  victory/draw.
- Match manager: create match, join as a player (claim a seat), get state,
  submit moves, list matches.
- Storage interface + in-memory implementation.
- HTTP REST server: create/list/join/get/move endpoints with JSON.
- WebSocket transport: subscribe to a match, get state pushes after every
  applied move.
- Example game: **tic-tac-toe**, fully wired through engine + server.
- Tests: unit tests for engine + match + tic-tac-toe rules; an
  HTTP-level integration test.
- README with quickstart.

## Explicitly out of scope for MVP

- Phases / stages / activePlayers (boardgame.io has a rich phase system).
- Secret state (`playerView`) and per-seat redaction.
- Random number plugin / seeded RNG.
- Undo / redo / replay log.
- Bots / AI.
- Persistent storage (Postgres, SQLite). Memory only.
- Authentication / credentials (boardgame.io issues per-seat credentials).
  We'll stub a `playerID` field but not verify it.
- Lobby UI / matchmaking UX.
- React client. We'll target a generic JSON-over-WS protocol.

These can be added after MVP without breaking the core API surface, which is
the point of keeping the engine pure.

## Package layout

```
boardgame-go/
├── go.mod
├── README.md
├── PLAN.md
├── core/                 # pure engine, no I/O
│   ├── game.go           # Game, Move types
│   ├── state.go          # State, Ctx
│   ├── reducer.go        # Apply
│   └── *_test.go
├── match/                # match lifecycle, joins, storage facade
│   ├── manager.go
│   └── manager_test.go
├── storage/              # storage interface + memory impl
│   ├── storage.go
│   └── memory.go
├── server/               # HTTP + WebSocket transport
│   ├── http.go
│   ├── ws.go
│   └── http_test.go
├── games/
│   └── tictactoe/
│       ├── tictactoe.go
│       └── tictactoe_test.go
└── cmd/
    └── boardgame-server/
        └── main.go       # runs tic-tac-toe server
```

## Wire protocol (MVP)

REST (JSON):

- `POST /games/{gameName}/create` → `{matchID}`
- `GET  /games/{gameName}/matches` → `[{matchID, players:[...]}]`
- `POST /games/{gameName}/{matchID}/join` body `{playerID?, name}` →
  `{playerID, seat}`
- `GET  /games/{gameName}/{matchID}/state` → `{G, ctx}`
- `POST /games/{gameName}/{matchID}/move`
  body `{playerID, move, args:[...]}` → `{G, ctx}` or `{error}`

WebSocket:

- `GET /games/{gameName}/{matchID}/ws` — server pushes `{G, ctx}` on every
  state change; client may send `{type:"move", move, args}` to submit.

## Test plan

- `core`: tic-tac-toe-shaped fake game asserts reducer + turn advance + EndIf.
- `match`: create, join (assigns seats round-robin), reject move from wrong
  player, reject move when seat unclaimed.
- `storage/memory`: round-trip a match.
- `games/tictactoe`: play a full game; assert winner; assert draw on full
  board; assert invalid placement rejected.
- `server`: spin up real http server in test, create/join/move via http,
  assert state. WebSocket happy path: connect, submit move, receive update.
- `cmd`: smoke run via curl in the README quickstart.

## Build sequence

1. Scaffold module + `core` types — no logic.
2. TDD core reducer with a tiny synthetic game.
3. Match manager + memory storage.
4. Tic-tac-toe on top of the core (proves the API is usable).
5. HTTP server with REST.
6. WebSocket layer reuses the match manager.
7. `cmd/boardgame-server` wires everything.
8. Smoke test + README.
