# Inherent JS/Node/React limitations Go solves

This file inventories the structural limits BGIO has *because* it's built
on JavaScript / Node.js / React, and what we can do about each in Go.
Distinct from [PARITY.md](PARITY.md) (close the feature gap) and
[GO_LEVERAGE.md](GO_LEVERAGE.md) (incremental Go wins). The items here
are wins that **BGIO structurally cannot match** without rewriting in a
different runtime.

## 1 — Single-threaded engine

**BGIO problem.** Node.js runs the engine on one event loop. A slow move
(heavy random, big Immer copy, custom plugin doing I/O) blocks every
other match on that process. Scaling means more processes + a sticky
load balancer.

**Go fix.** Goroutines. Different matches naturally run in parallel —
the manager already serialises *per-match* writes via `sync.Map`-keyed
locks, but cross-match throughput is bounded only by available cores.
**No code change needed**; this is a runtime fact. We should benchmark
and document it as a marketing point.

## 2 — Slow subscribers block broadcasts

**BGIO problem.** Socket.io blocks the move pipeline if a subscriber's
write buffer is full. Slow clients can hold up the whole match.

**Our current state.** `Manager.broadcast` calls each subscriber's
`Send` synchronously, *under the per-match lock*. A slow WS client can
stall subsequent moves on the same match.

**Go fix (shipping).** Per-subscriber bounded send queue with
drop-oldest semantics. State pushes are idempotent — only the latest
state matters — so dropping is correct.

## 3 — No deterministic replay

**BGIO problem.** Replay sort-of works but breaks when moves call
`Date.now()` or `Math.random()`. There's no enforced determinism
boundary.

**Our current state.** Random is gated by the seeded plugin. The reducer
is pure given (G, Ctx, args).

**Go fix (shipping).** `core.Replay(game, log) State` — given a recorded
log, replay deterministically from `NewMatch`. The reconstructed final
state should byte-match the original. Useful for: post-game analysis,
debugging, AI training corpora, bug repros via match dumps.

## 4 — No server-side bot workers

**BGIO problem.** BGIO has Random and MCTS bots but they run in the
same JS engine as the game master. Node's single thread caps MCTS depth
in real time — you can't run 1000 rollouts mid-turn without stalling
real players.

**Go fix (shipping).** `bots` package with `RandomBot` / `MCTSBot` that
run as separate goroutines (worker pool). MCTS in Go is ~10-50x faster
than the JS equivalent; we can run deep rollouts in milliseconds per
turn. Bots subscribe to a match like any client and submit moves via
`Manager.MoveReq` — no special engine path.

## 5 — No match TTL / janitor

**BGIO problem.** Memory accumulates dead matches. The Lobby example
suggests cron-cleaning, but there's nothing built in.

**Go fix (shipping).** `Manager.RunJanitor(ctx, opts)` is a background
goroutine scanning storage and wiping matches idle for longer than
`opts.IdleAfter`. Single goroutine; cheap; works against any backend.

## 6 — Schema evolution

**BGIO problem.** When a game definition changes (new G field, removed
move), persisted matches break. There's no migration story.

**Go fix (proposed).** `Game.SchemaVersion int` + `Game.Migrate(state,
fromVersion) State`. Storage adapters record the version with the match;
on load, if version < current, run the migration chain. Documented but
not shipped this pass — flagged for follow-up.

## 7 — Turn timer / chess clock

**BGIO problem.** Common request (issue #92, open since 2017). Requires
a per-match setTimeout in Node; doesn't compose well with the
single-threaded event loop.

**Go fix (proposed).** `Turn.TimeBudget time.Duration` + a single
janitor goroutine running per-match timers via `time.AfterFunc`.
`Manager.AutoExpire(matchID)` force-ends an expired turn. Documented
but not shipped this pass — flagged for follow-up.

## 8 — V8 GC pauses under load

**BGIO problem.** V8 GC has unpredictable pauses; under heavy match
load this shows up as latency spikes on state pushes.

**Go fix.** Go's GC is sub-millisecond by default. No code change —
documented as a marketing point.

## 9 — Native binary distribution

**BGIO problem.** Requires Node runtime + npm install pipeline.
Deploying to a constrained edge environment (a Raspberry Pi, a
container with no package manager) is awkward.

**Go fix.** Single static binary cross-compiled for any arch.
Already true. Marketing point.

## 10 — Determinism linter

**BGIO problem.** Nothing stops a move from calling `Date.now()`. The
framework relies on convention.

**Go fix (proposed).** `cmd/boardgame-go-vet` — a `go vet`-style
analyser that flags `time.Now`, `math/rand`, and any other syscall that
breaks reproducibility inside `MoveFn`/`HookFn` bodies. Documented but
not shipped this pass — would be a separate sub-binary.

## Shipping in this pass

The four wins that are concrete code changes with high value-per-LOC:

1. Slow-subscriber back-pressure (real bug fix)
2. Deterministic replay (`core.Replay`)
3. Match janitor (`Manager.RunJanitor`)
4. Server-side bots (RandomBot + MCTSBot)

Deferred but documented above (#6, #7, #10).
