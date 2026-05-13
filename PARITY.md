# boardgame.io Parity Checklist

Drift list of every BGIO feature we need to match. **All in-scope items
shipped.** This document also tracks the 22+ BGIO open issues we've
addressed (engine bugs, missing features, parity gaps) — search for
`BGIO #` in the test suite for one regression test per issue.

For features that go beyond BGIO parity, see
[GO_LEVERAGE.md](GO_LEVERAGE.md) and [JS_LIMITATIONS.md](JS_LIMITATIONS.md).

What's **explicitly skipped**: client-side JS code (React/Plain JS
`Client`, `<Lobby/>` component, Redux integration, the in-browser Debug
Panel). Those are intrinsically JavaScript and have no Go analogue. The
wire protocol on the other hand is in scope — any Go (or JS, or
anything) client can drive a `boardgame-go` server.

## 1 — Game definition

- [x] `Name`
- [x] `MinPlayers` / `MaxPlayers`
- [x] `Setup(ctx, setupData) G`
- [x] `ValidateSetupData(setupData, numPlayers) string`
- [x] `Seed` (string or int — seeds the Random plugin)
- [x] `DisableUndo` flag
- [x] `DeltaState` flag (JSON Patch diffs in transport)
- [x] `Moves` short-form (`name → MoveFn`)
- [x] `Moves` long-form (`Move{Move, Undoable, Redact, ServerOnly, NoLimit, IgnoreStaleStateID, IgnoreBlocks, Timeout}`)
- [x] `EndIf(mc) any` — value becomes `ctx.Gameover`
- [x] `OnEnd(mc) G` — runs when the game ends
- [x] `OnUndo(mc) G` — intercept undo to scrub transient fields (BGIO #1135)
- [x] `PlayerView(g, ctx, playerID) G`
- [x] `Plugins []Plugin`
- [x] `Enumerate` — legal-actions function for bots / AI (BGIO #1078)
- [x] `Validate()` — catches config bugs at registration time
- [x] `SchemaVersion` + `Migrate` — schema evolution for persisted matches
- [x] `AllowDynamicPlayers` — mid-match join (BGIO #884, #1102)
- [x] `SpectatorsAllowed` — lock matches to seated players (BGIO #1007)

## 2 — Turn config

- [x] `Turn.Order` — TurnOrder strategy
- [x] `Turn.OnBegin(mc) G`
- [x] `Turn.OnEnd(mc) G`
- [x] `Turn.OnMove(mc) G`
- [x] `Turn.MinMoves`
- [x] `Turn.MaxMoves`
- [x] `Turn.TimeBudget` — engine-driven force-end (BGIO #92)
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
- [x] `Events.EndTurn(next)` ad-hoc override (survives MaxMoves — BGIO #924)

## 4 — Phases

- [x] `Phases` map on Game
- [x] `ctx.Phase` field
- [x] `Phase.Start`
- [x] `Phase.Next` (string or `func(mc) string`)
- [x] `Phase.OnBegin / OnEnd / EndIf`
- [x] `Phase.Moves` override
- [x] `Phase.Turn` override
- [x] Hook event drain (BGIO #1237 — endTurn in phase.OnBegin no longer dropped)

## 5 — Move long-form

- [x] `Move.Move`
- [x] `Move.Undoable` (bool or func)
- [x] `Move.Redact` (bool or func, hidden in log to other players)
- [x] `Move.ServerOnly` (BGIO's `client: false`)
- [x] `Move.NoLimit` (skip min/max counting)
- [x] `Move.IgnoreStaleStateID`
- [x] `Move.IgnoreBlocks` (concede / forfeit can bypass cascade pause)
- [x] `Move.Timeout` (cooperative cancellation via context)

## 6 — Events (callable from moves / hooks)

- [x] `Events.EndTurn()` / `EndTurn(next)`
- [x] `Events.Pass()`
- [x] `Events.EndPhase()`
- [x] `Events.SetPhase(name)`
- [x] `Events.EndStage()` (uses Stage.Next)
- [x] `Events.SetStage(name)` and `SetStageLong(stage, min, max)`
- [x] `Events.EndGame(result)` writes ctx.Gameover + runs OnEnd
- [x] `Events.SetActivePlayers(cfg)` with full BGIO option set
- [x] `Events.RemovePlayer(playerID)` — eliminate from PlayOrder (BGIO #616)
- [x] `Events.RunMove(name, args)` — synchronous chained dispatch (BGIO #1085)
- [x] `core.ActivePlayersInOrder([]string, ...)` — sequential prompts (BGIO #478)

## 7 — Stages & active players

- [x] `ctx.ActivePlayers` map (serialised as `{}` not `null` — BGIO #777)
- [x] `StageNull` sentinel + `core.Stage(name)` helper
- [x] `ActivePlayersAll` / `AllOnce` / `Others` / `OthersOnce` presets
- [x] `Revert` and `Next` follow-up `ActivePlayersConfig`
- [x] Per-player Min/MaxMoves overrides
- [x] Stage-level Moves override
- [x] `Stage.OnBegin` / `Stage.OnEnd` hooks (BGIO #608)

## 8 — Random plugin

- [x] `plugins/random` package
- [x] Seeded splitmix64 PRNG (string or number seed)
- [x] `Die(sides[, count])` / `D4`-`D20` / `Number()` / generic `Shuffle[T]`
- [x] State stored in plugin private data, stripped via `PlayerView`
- [x] `mc.Random` shortcut wired in core
- [x] `testhelpers.MockRandom` for deterministic overrides
- [x] TurnOrder callbacks intentionally don't expose plugins (BGIO #1131)

## 9 — Secret state / PlayerView

- [x] `Game.PlayerView` hook
- [x] `core.StripSecrets` helper (BGIO's `PlayerView.STRIP_SECRETS`)
- [x] Per-seat redacted state pushed by transport

## 10 — Plugin API

- [x] `core.Plugin` + optional sub-interfaces: `PluginSetup`, `PluginAPI`,
      `PluginFlush`, `PluginInvalid`, `PluginPlayerView`
- [x] `PluginFnWrapMove` / `PluginFnWrapHook` with `GameMethod` enum
- [x] Setup runs at NewMatch; APIs exposed via `mc.Plugins[name]`
- [x] Flush persists mutations after each move
- [x] IsInvalid rejects the move
- [x] PlayerView filters plugin data per seat
- [x] `plugins/player.PluginPlayer` built-in (per-seat records + Opponent)

## 11 — Undo / redo / log

- [x] Move log (`State.Log`) with kind/move/playerID/args/turn/phase
- [x] Hierarchical log via `LogEntry.Parent` for drain-step children
- [x] `mc.AddLog` from inside hooks (BGIO #1228 — bus didn't see them)
- [x] `core.Undo` / `core.Redo` (scope: current turn)
- [x] `core.CanUndo` / `core.CanRedo` predicates (BGIO #1048)
- [x] `Game.DisableUndo` + per-move `Undoable`
- [x] Log redaction per player honouring `Move.Redact`
- [x] `core.Replay(game, log, …)` / `ReplayUntil(n)` — deterministic
      replay through any step (BGIO #892)

## 12 — Game state context (ctx) parity

- [x] `Turn`
- [x] `CurrentPlayer`
- [x] `NumPlayers`
- [x] `PlayOrder`
- [x] `PlayOrderPos`
- [x] `Phase`
- [x] `ActivePlayers` (always `{}` not `null` on the wire)
- [x] `Gameover`

## 13 — Server / Lobby API

REST routes shipped:

- [x] `GET  /games` — list registered game names
- [x] `GET  /games/{name}` — list matches (filters unlisted)
- [x] `GET  /games/{name}/{id}` — get one match
- [x] `GET  /games/{name}/byCode/{code}` — lookup by invite code (BGIO #574)
- [x] `POST /games/{name}/create` — create match (numPlayers, setupData, unlisted, name, joinCode)
- [x] `POST /games/{name}/{id}/join` — join (playerName, playerID, data)
- [x] `POST /games/{name}/{id}/leave` — leave (credentials)
- [x] `POST /games/{name}/{id}/update` — rename / data (credentials; broadcasts matchData — BGIO #877)
- [x] `POST /games/{name}/{id}/playAgain` — successor match (idempotent)
- [x] `POST /games/{name}/{id}/move` — move (REST; also via WS)
- [x] `POST /games/{name}/{id}/dryMove` — preview without persisting (BGIO #636)
- [x] `POST /games/{name}/{id}/reset` — re-init keeping seated players (BGIO #1166)
- [x] Per-seat credentials (16-byte hex by default)
- [x] Pluggable `GenerateCredentials` / `AuthenticateCredentials`
- [x] `Origins` CORS preset (`LOCALHOST_IN_DEVELOPMENT`, `LOCALHOST`, literals, `*`)
- [x] `unlisted` matches hidden from list
- [x] Match metadata in responses (isConnected, name, data, ctx, setupData, joinCode)
- [x] Chat messages via WS frames
- [x] `nextMatchID` on the source match after playAgain
- [x] `Server.MaxBodyBytes` cap (BGIO #860)
- [x] HTTPS via `Server.RunTLS` / `RunTLSConfig`

## 14 — Multiplayer transport

- [x] WebSocket transport with per-seat redacted state
- [x] BGIO-style frames: `sync` (initial), `update`, `matchData`, `chat`, `patch`, `error`
- [x] `matchData` broadcast on join/leave/update
- [x] `?playerID=` query binds the WS to a seat (spectator on empty)
- [x] DeltaState: `patch` frames with RFC 6902 JSON Patch
- [x] State IDs + stale-state rejection (`ErrStaleState`)
- [x] `Manager.RequireStateID` strict-mode flag
- [x] Heartbeat (25 s ping/pong) + `IsConnected` flips on socket lifecycle
- [x] Per-connection bounded send queue (drop-oldest backpressure)
- [x] Concurrent simultaneous joins safe under match lock (BGIO #1100)

## 15 — Storage

- [x] In-memory storage
- [x] FlatFile (JSON file per match, atomic rename writes)
- [x] SQLite (pure Go via modernc.org/sqlite, no CGO)
- [x] Postgres (pgx-via-database/sql; OptimisticStorage capable)
- [x] Redis (go-redis; per-game and all-matches sets back List())
- [x] `Cached` decorator (TTL + LRU write-through)
- [x] `WriteBehind` decorator (async batched flushes with per-match coalescing)
- [x] `Invalidator` interface + Redis impl (cross-node cache busts)
- [x] `Wipe(id)` on the Storage interface
- [x] `OptimisticStorage` interface for multi-instance OCC scaling
- [x] Concurrency-safe in-process locks

## 16 — Bots / AI

- [x] `bots.Bot` interface (Play returns Action)
- [x] `RandomBot` baseline
- [x] `MCTSBot` with UCT selection, random rollouts, early-stop callback (BGIO #906)
- [x] `MCTSBot.Perspective` runs rollouts against `PlayerView` (BGIO #1069)
- [x] `AutoPlayer` runs a bot against a live match as Subscriber
- [x] `bots.Simulate` for round-robin matches + stats (BGIO #494, #439)
- [x] `bots/llm/` — LLM-backed bots (OpenAI / OpenRouter / Anthropic)

## 17 — Testing helpers

- [x] `testhelpers.Scenario` — build state for snapshot tests
- [x] `testhelpers.MockRandom` — override D6/Die/Number/Shuffle per test
- [x] `cmd/boardgame-go-vet` — `go vet` analyser flagging non-deterministic
      calls inside MoveFn/HookFn/EndIfFn bodies

## 18 — Manager lifecycle

- [x] `Manager.OnLifecycle` / `OnLifecycleKind` for
      created/joined/left/moved/gameOver/reset events
- [x] `Manager.Reset(matchID, ...)` (BGIO #1166)
- [x] `Manager.DryMove` / `DryMoveReq` (BGIO #636)
- [x] `Manager.ExportMatch` / `ImportMatch` portable replay bundles
- [x] `Manager.AutoExpire(matchID, expectedTurn)` for turn timer firing
- [x] `Manager.RunJanitor(ctx, opts)` background sweep
- [x] `Manager.UseOptimisticConcurrency` for multi-instance scaling
- [x] `Manager.FindByJoinCode` (BGIO #574)
- [x] `Manager.MustRegister(game)` panics on `Game.Validate()` failure

## 19 — Explicitly out of scope (intrinsically JS)

- React `<Client/>` / `<Lobby/>` components
- Redux store / enhancer / DevTools integration
- In-browser Debug Panel
- Plain JS `Client` (Go has no equivalent — anyone can hit the wire
  protocol from any language)
- `LobbyClient` JS wrapper

A standalone TS transport library is filed as
[issue #6](https://github.com/tjcran/boardgame-go/issues/6) with the
cross-language maintenance trade-off; revisit when there's concrete
demand.

## Beyond BGIO

Features in our port that BGIO doesn't have. See README's comparison
table or [JS_LIMITATIONS.md](JS_LIMITATIONS.md) for the full
analysis.

- **Action queue + drain primitive** (`mc.Queue.Push` / `Block` with
  `ResumeTag` matching) for MTG-style trigger cascades with pause/resume
- **`ccg/` library** — CCG/TCG bookkeeping (entities, zones, layered
  modifiers, sync event bus, target queries)
- **Compile-time-typed games** via `typedgame.Game[S]` generics
- **Cross-match concurrency** by default (goroutines vs Node event loop)
- **Server-side bots** that don't block other matches; MCTS rollouts
  parallelise across goroutines
- **`MCTSBot.Perspective`** prevents AIs from cheating via opponent
  hidden state
- **`bots/llm/`** — LLM bots with tool-calling first (OpenAI /
  OpenRouter / Anthropic)
- **Single static binary** with no Node runtime
- **`go vet` determinism analyser** enforcing the no-`time.Now`/-`rand`
  contract at compile time
- **Optimistic-concurrency storage interface** for multi-instance
  deployments without sticky sessions
- **Storage decorators** (Cached, WriteBehind, Invalidator) wrapping any
  backend
