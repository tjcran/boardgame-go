# boardgame.io Parity Checklist

Drift list of every BGIO feature we need to match. The goal is full parity
with `main` of [boardgame.io](https://github.com/boardgameio/boardgame.io),
no extras. Items are checked off as they ship in `boardgame-go`.

What is **explicitly skipped**: client-side JS code (React/Plain JS `Client`,
`<Lobby/>` component, Redux integration, the in-browser Debug Panel). Those
are intrinsically JavaScript and have no Go analogue. The wire protocol on
the other hand is in scope — any Go (or JS, or anything) client should be
able to drive a `boardgame-go` server.

## 1 — Game definition

- [x] `Name`
- [x] `MinPlayers` / `MaxPlayers`
- [x] `Setup(numPlayers) G`
- [ ] `Setup(ctx, setupData) G` — accept `setupData` from match creation
- [ ] `ValidateSetupData(setupData, numPlayers) error`
- [ ] `Seed` (string or int — seeds the Random plugin)
- [ ] `DisableUndo` flag
- [ ] `DeltaState` flag (JSON Patch diffs in transport)
- [x] `Moves` (short-form: name → MoveFn)
- [ ] `Moves` (long-form: per-move config — see §5)
- [x] `EndIf(g, ctx) -> bool, winner, draw` (current shape)
- [ ] `EndIf(g, ctx) -> any` — value becomes `ctx.Gameover` per BGIO
- [ ] `OnEnd(g, ctx) G` — runs when the game ends
- [ ] `PlayerView(g, ctx, playerID) G` — per-seat redaction
- [ ] `Plugins []Plugin`

## 2 — Turn config (per phase or global)

- [ ] `Turn.Order` — TurnOrder strategy
- [ ] `Turn.OnBegin(g, ctx) G`
- [ ] `Turn.OnEnd(g, ctx) G`
- [ ] `Turn.OnMove(g, ctx) G`
- [ ] `Turn.MinMoves`
- [ ] `Turn.MaxMoves`
- [ ] `Turn.EndIf(g, ctx) -> bool | { Next: playerID }`
- [ ] `Turn.ActivePlayers` — initial activePlayers when phase/turn starts
- [ ] `Turn.Stages` — stage map for intra-turn sub-states

## 3 — Turn order strategies

- [x] DEFAULT — round-robin (currently the only one)
- [ ] RESET — round-robin, restart at index 0 on phase start
- [ ] CONTINUE — start with the player who ended the previous phase
- [ ] ONCE — round-robin once then auto-end phase
- [ ] CUSTOM(order) — explicit play order
- [ ] CUSTOM_FROM(propertyOfG) — play order taken from G
- [ ] Custom turn order: `{First, Next, PlayOrder}` callbacks
- [ ] `endTurn({next: playerID})` ad hoc override

## 4 — Phases

- [ ] `Phases` map on Game
- [ ] `ctx.Phase` field
- [ ] `Phase.Start` (initial phase flag)
- [ ] `Phase.Next` (string or `func(g,ctx) string`)
- [ ] `Phase.OnBegin(g, ctx) G`
- [ ] `Phase.OnEnd(g, ctx) G`
- [ ] `Phase.EndIf(g, ctx) -> bool | {Next: phaseName}`
- [ ] `Phase.Moves` (phase-local move table)
- [ ] `Phase.Turn` (phase-local turn config)

## 5 — Move long-form options

- [ ] `Move.Move` (the function itself)
- [ ] `Move.Undoable` — bool or function
- [ ] `Move.Redact` — hide args in log to other players
- [ ] `Move.NoLimit` — exempt from minMoves/maxMoves
- [ ] `Move.Client` — `false` to run only on server (secret-state moves)
- [ ] `Move.IgnoreStaleStateID` — allow move from out-of-date clients

## 6 — Events available inside moves/hooks

- [ ] `events.EndTurn()` and `events.EndTurn({Next})`
- [ ] `events.EndPhase()` (uses Phase.Next)
- [ ] `events.SetPhase(name)`
- [ ] `events.EndStage()` (uses Stage.Next)
- [ ] `events.SetStage(name)` and long-form `{Stage, MinMoves, MaxMoves}`
- [ ] `events.EndGame(result)`
- [ ] `events.SetActivePlayers(config)` with full BGIO option set
- [ ] `events.Pass()` (ends turn without action)
- [ ] Event-availability matrix per hook (e.g. no endTurn inside onEnd)

## 7 — Stages & active players

- [ ] `ctx.ActivePlayers` map[playerID]stageName
- [ ] `Stage.NULL` sentinel (in active set, no stage filter)
- [ ] `ActivePlayers.ALL` preset
- [ ] `ActivePlayers.ALL_ONCE` preset
- [ ] `ActivePlayers.OTHERS` preset
- [ ] `ActivePlayers.OTHERS_ONCE` preset
- [ ] `ActivePlayers.CURRENT_PLAYER` preset
- [ ] `Revert` / `Next` follow-up `ActivePlayers` configs
- [ ] Stage-level Min/MaxMoves
- [ ] Stage-level Moves override

## 8 — Random plugin

- [ ] Seeded PRNG (uses Game.Seed)
- [ ] `random.Die(sides[, count])`
- [ ] `random.D4/D6/D8/D10/D12/D20([count])`
- [ ] `random.Number()` -> float in [0,1)
- [ ] `random.Shuffle([]T) []T`
- [ ] PRNG state hidden from clients
- [ ] `MockRandom` deterministic test override

## 9 — Secret state / PlayerView

- [ ] `Game.PlayerView` hook
- [ ] Built-in `PlayerView.STRIP_SECRETS` (drops `secret` key, hides other
      players' `players` slot)

## 10 — Plugin API

- [ ] `Plugin.Name`
- [ ] `Plugin.Setup(ctx) data`
- [ ] `Plugin.API(data, ctx) api` — exposed to moves as `ctx[name]`
- [ ] `Plugin.Flush(api, data) data` — persists changes
- [ ] `Plugin.FnWrap(fn, fnType) fn` — wraps moves/hooks
- [ ] `Plugin.NoClient() bool`
- [ ] `Plugin.IsInvalid(data) error`
- [ ] `Plugin.PlayerView(data, ctx, playerID) data`
- [ ] Plugin order (left-to-right)
- [ ] `PluginPlayer` built-in (per-player state with opponent helper)

## 11 — Undo / redo / log

- [ ] Move log (recorded moves with metadata)
- [ ] `client.Undo()` semantics (within current turn)
- [ ] `client.Redo()`
- [ ] `Game.DisableUndo`
- [ ] Move-level `Undoable` flag/function
- [ ] Log redaction per player (uses `Move.Redact`)

## 12 — Game state context (ctx) parity

- [x] `Turn`
- [x] `CurrentPlayer`
- [x] `NumPlayers`
- [x] `PlayOrder`
- [ ] `PlayOrderPos`
- [ ] `Phase`
- [ ] `ActivePlayers`
- [x] `Gameover` (currently split into Winner/IsDraw — see §1)

## 13 — Server / Lobby API

REST routes BGIO ships:

- [ ] `GET  /games` — list registered game names
- [x] `GET  /games/{name}` — list matches *(currently `/matches`)*
- [ ] `GET  /games/{name}/{id}` — get one match
- [x] `POST /games/{name}/create` — create a match
- [ ] body: `numPlayers`, `setupData`, `unlisted`
- [x] `POST /games/{name}/{id}/join` — join
- [ ] body: `playerName` (we use `name`), returns `playerCredentials`
- [ ] `POST /games/{name}/{id}/leave`
- [ ] `POST /games/{name}/{id}/update` — rename / data
- [ ] `POST /games/{name}/{id}/playAgain`
- [ ] Credentials per seat (generateCredentials/authenticateCredentials)
- [ ] `apiOrigins` / `Origins` CORS preset
- [ ] HTTPS support
- [ ] Custom routes hook (mux exposed)
- [ ] `unlisted` matches (hidden from list)
- [ ] Per-match metadata (`isConnected`, etc.)
- [ ] Chat messages

## 14 — Multiplayer transport

- [x] WebSocket transport with state pushes
- [ ] Frame protocol parity with BGIO (sync, update, matchData, chat,
      patch frames)
- [ ] `deltaState` JSON Patch frames
- [ ] Disconnect / reconnect detection (`isConnected` per seat)
- [ ] Heartbeat
- [ ] Spectator mode (connect without a seat)
- [ ] Optimistic client moves (server is authoritative, sends correction)

## 15 — Storage

- [x] In-memory storage
- [ ] FlatFile (file per match, similar to node-persist)
- [ ] Async storage interface (already sync-only; BGIO is async)
- [ ] Match metadata stored separately from game state (BGIO splits these)

## 16 — Testing helpers

- [ ] Scenario helper: build a `State` directly for snapshot testing
- [ ] `Local()` transport equivalent for in-process multi-client tests
- [ ] `MockRandom` for deterministic tests

## 17 — Explicitly out of scope (intrinsically JS)

- React `<Client/>` / `<Lobby/>` components
- Redux store / enhancer / DevTools integration
- In-browser Debug Panel
- Plain JS `Client` (Go has no equivalent — anyone can hit the wire
  protocol from any language)
- `LobbyClient` JS wrapper

## Implementation order

Picked by dependency, not importance:

1. Expanded `Ctx` (`Phase`, `PlayOrderPos`, `ActivePlayers`, `Gameover` as
   `any`).
2. Long-form `Move` + per-move flags scaffolding.
3. Turn config struct (`Turn.OnBegin/OnEnd/OnMove`, min/max moves, endIf,
   order).
4. Turn-order strategies.
5. Events system (endTurn/endPhase/endGame/setStage/setPhase/setActivePlayers/pass).
6. Phases (routing + phase-local moves/turn).
7. Stages & ActivePlayers presets.
8. Plugin API skeleton.
9. Random plugin (seeded PRNG) on top of plugin API.
10. PlayerView (game-level + plugin-level).
11. PluginPlayer built-in.
12. Undo/redo + move log.
13. `EndGame` event + `OnEnd` hook.
14. Move flags wired (Redact, Undoable, NoLimit, Client, IgnoreStaleStateID).
15. Server features: credentials, full Lobby routes, metadata, chat.
16. Transport frame parity (sync/update/patch/matchData/chat).
17. DeltaState (JSON Patch).
18. Storage: FlatFile, async interface, split metadata.
19. Testing helpers.
