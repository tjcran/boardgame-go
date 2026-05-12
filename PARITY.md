# boardgame.io Parity Checklist

Drift list of every BGIO feature we need to match. Items are checked off as
they ship in `boardgame-go`.

What is **explicitly skipped**: client-side JS code (React/Plain JS `Client`,
`<Lobby/>` component, Redux integration, the in-browser Debug Panel). Those
are intrinsically JavaScript and have no Go analogue. The wire protocol on
the other hand is in scope — any Go (or JS, or anything) client can drive a
`boardgame-go` server.

## 1 — Game definition

- [x] `Name`
- [x] `MinPlayers` / `MaxPlayers`
- [x] `Setup(ctx, setupData) G`
- [x] `ValidateSetupData(setupData, numPlayers) string`
- [x] `Seed` (string or int — seeds the Random plugin)
- [x] `DisableUndo` flag
- [x] `DeltaState` flag (JSON Patch diffs in transport)
- [x] `Moves` short-form (`name → MoveFn`)
- [x] `Moves` long-form (`Move{Move, Undoable, Redact, ServerOnly, NoLimit, IgnoreStaleStateID}`)
- [x] `EndIf(mc) any` — value becomes `ctx.Gameover`
- [x] `OnEnd(mc) G` — runs when the game ends
- [x] `PlayerView(g, ctx, playerID) G`
- [x] `Plugins []Plugin`

## 2 — Turn config

- [x] `Turn.Order` — TurnOrder strategy
- [x] `Turn.OnBegin(mc) G`
- [x] `Turn.OnEnd(mc) G`
- [x] `Turn.OnMove(mc) G`
- [x] `Turn.MinMoves`
- [x] `Turn.MaxMoves`
- [x] `Turn.EndIf(mc) -> (end, next)`
- [x] `Turn.ActivePlayers`
- [x] `Turn.Stages`

## 3 — Turn order strategies

- [x] `TurnOrderDefault`
- [x] `TurnOrderReset`
- [x] `TurnOrderContinue`
- [x] `TurnOrderOnce`
- [x] `TurnOrderCustom(order)`
- [x] `TurnOrderCustomFrom(func(g) []string)`
- [x] Custom `TurnOrder{First, Next, PlayOrder}`
- [x] `Events.EndTurn(next)` ad-hoc override

## 4 — Phases

- [x] `Phases` map on Game
- [x] `ctx.Phase` field
- [x] `Phase.Start`
- [x] `Phase.Next` (string or `func(mc) string`)
- [x] `Phase.OnBegin / OnEnd / EndIf`
- [x] `Phase.Moves` override
- [x] `Phase.Turn` override

## 5 — Move long-form

- [x] `Move.Move`
- [x] `Move.Undoable` (bool or func)
- [x] `Move.Redact` (bool or func, hidden in log to other players)
- [x] `Move.ServerOnly` (BGIO's `client: false`)
- [x] `Move.NoLimit` (skip min/max counting)
- [x] `Move.IgnoreStaleStateID`

## 6 — Events (callable from moves / hooks)

- [x] `Events.EndTurn()` / `EndTurn(next)`
- [x] `Events.Pass()`
- [x] `Events.EndPhase()`
- [x] `Events.SetPhase(name)`
- [x] `Events.EndStage()` (uses Stage.Next)
- [x] `Events.SetStage(name)` and `SetStageLong(stage, min, max)`
- [x] `Events.EndGame(result)` writes ctx.Gameover + runs OnEnd
- [x] `Events.SetActivePlayers(cfg)` with full BGIO option set

## 7 — Stages & active players

- [x] `ctx.ActivePlayers` map
- [x] `StageNull` sentinel + `core.Stage(name)` helper
- [x] `ActivePlayersAll` / `AllOnce` / `Others` / `OthersOnce` presets
- [x] `Revert` and `Next` follow-up `ActivePlayersConfig`
- [x] Per-player Min/MaxMoves overrides
- [x] Stage-level Moves override

## 8 — Random plugin

- [x] `plugins/random` package
- [x] Seeded splitmix64 PRNG (string or number seed)
- [x] `Die(sides[, count])` / `D4`-`D20` / `Number()` / generic `Shuffle[T]`
- [x] State stored in plugin private data, stripped via `PlayerView`
- [x] `mc.Random` shortcut wired in core
- [x] `testhelpers.MockRandom` for deterministic overrides

## 9 — Secret state / PlayerView

- [x] `Game.PlayerView` hook
- [x] `core.StripSecrets` helper (BGIO's `PlayerView.STRIP_SECRETS`)
- [x] Per-seat redacted state pushed by transport

## 10 — Plugin API

- [x] `core.Plugin` + optional sub-interfaces: `PluginSetup`, `PluginAPI`,
      `PluginFlush`, `PluginInvalid`, `PluginPlayerView`
- [x] Setup runs at NewMatch; APIs exposed via `mc.Plugins[name]`
- [x] Flush persists mutations after each move
- [x] IsInvalid rejects the move
- [x] PlayerView filters plugin data per seat
- [x] `plugins/player.PluginPlayer` built-in (per-seat records + Opponent)

## 11 — Undo / redo / log

- [x] Move log (`State.Log`) with kind/move/playerID/args/turn/phase
- [x] `core.Undo` / `core.Redo` (scope: current turn)
- [x] `Game.DisableUndo` + per-move `Undoable`
- [x] Log redaction per player honouring `Move.Redact`

## 12 — Game state context (ctx) parity

- [x] `Turn`
- [x] `CurrentPlayer`
- [x] `NumPlayers`
- [x] `PlayOrder`
- [x] `PlayOrderPos`
- [x] `Phase`
- [x] `ActivePlayers`
- [x] `Gameover`

## 13 — Server / Lobby API

REST routes shipped:

- [x] `GET  /games` — list registered game names
- [x] `GET  /games/{name}` — list matches (filters unlisted)
- [x] `GET  /games/{name}/{id}` — get one match
- [x] `POST /games/{name}/create` — create match (numPlayers, setupData, unlisted)
- [x] `POST /games/{name}/{id}/join` — join (playerName, playerID, data)
- [x] `POST /games/{name}/{id}/leave` — leave (credentials)
- [x] `POST /games/{name}/{id}/update` — rename / data (credentials)
- [x] `POST /games/{name}/{id}/playAgain` — successor match (idempotent)
- [x] `POST /games/{name}/{id}/move` — move (REST; also via WS)
- [x] Per-seat credentials (16-byte hex by default)
- [x] Pluggable `GenerateCredentials` / `AuthenticateCredentials`
- [x] `Origins` CORS preset (`LOCALHOST_IN_DEVELOPMENT`, `LOCALHOST`, literals, `*`)
- [x] `unlisted` matches hidden from list
- [x] Match metadata in responses (isConnected, name, data, ctx, setupData)
- [x] Chat messages via WS frames
- [x] `nextMatchID` on the source match after playAgain

## 14 — Multiplayer transport

- [x] WebSocket transport with per-seat redacted state
- [x] BGIO-style frames: `sync` (initial), `update`, `matchData`, `chat`, `patch`, `error`
- [x] `matchData` broadcast on join/leave/update
- [x] `?playerID=` query binds the WS to a seat (spectator on empty)
- [x] DeltaState: `patch` frames with RFC 6902 JSON Patch
- [x] State IDs + stale-state rejection (`ErrStaleState`)
- [ ] Heartbeat / disconnect → `isConnected` tracking (TODO)

## 15 — Storage

- [x] In-memory storage
- [x] FlatFile (JSON file per match, atomic rename writes)
- [x] `Wipe(id)` on the Storage interface
- [x] Concurrency-safe in-process locks

## 16 — Testing helpers

- [x] `testhelpers.Scenario` — build state for snapshot tests
- [x] `testhelpers.MockRandom` — override D6/Die/Number/Shuffle per test

## 17 — Explicitly out of scope (intrinsically JS)

- React `<Client/>` / `<Lobby/>` components
- Redux store / enhancer / DevTools integration
- In-browser Debug Panel
- Plain JS `Client` (Go has no equivalent — anyone can hit the wire
  protocol from any language)
- `LobbyClient` JS wrapper

## Pending follow-ups

The features below aren't part of MVP parity but would be nice next steps:

- **Heartbeat / connection tracking.** BGIO tracks `isConnected` per seat by
  watching socket lifecycle. The plumbing for `IsConnected` is wired in the
  storage `Player` struct and `matchData` frame; the actual heartbeat
  observer is not.
- **`fnWrap` plugin hook.** The other plugin lifecycle hooks are wired; the
  per-method wrapper isn't. Add when a concrete use case shows up.
- **HTTPS server config.** `net/http`'s `ServeTLS` works today; we just
  haven't exposed a sugary `Server{HTTPS: {cert, key}}` config object.
- **Persistent SQL storage adapters.** FlatFile covers dev/test; a SQLite
  adapter would round out the BGIO equivalents.
