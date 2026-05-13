# BGIO flaws Go can fix

boardgame.io was built for the JS ecosystem and inherits some of its
ergonomic and runtime weak spots. This document inventories the gaps the
Go runtime, type system, and stdlib let us close. Each entry is scored:

- **Win**: clearly better in Go, low cost — ship it.
- **Maybe**: meaningful win but real cost (API churn, new deps,
  scope creep) — propose, decide before shipping.
- **Skip**: technically possible but not worth the cost vs. parity.

## 1 — Type-safe `G` via generics  **Win**

**BGIO flaw.** `G` is a JS object — completely untyped at runtime. TypeScript
types help in the IDE but the framework itself doesn't enforce them and a
single bad cast can spread through every move.

In our current Go port `G` is `any` for parity — moves assert `mc.G.(*MyG)`
on every call.

**Go fix.** Generics. `Game[S]`, `MoveFn[S] func(*MoveContext[S], ...any) (S, error)`,
`Setup[S] func(Ctx, any) S`. The compiler enforces type-correctness; no
runtime assertions. Plugins keep working because the engine internals are
still `any` — the generic shell is a thin compile-time wrapper.

**Cost.** A typed-game API alongside the existing untyped one. Old games
keep working; new games can opt into the typed surface for free.

## 2 — `context.Context` propagation through `Apply`  **Win**

**BGIO flaw.** No cancellation. A long-running move (heavy AI, big shuffle,
external lookup in a custom plugin) can't be cancelled; the server-side
event loop is the only thing that can break out, and even then it can't
propagate a deadline to the move code.

**Go fix.** Pass `context.Context` through `Apply` and into `MoveContext`.
Moves that need deadlines or tracing get them automatically. Existing
moves ignore the field. Trivial change, zero migration pain for callers
that don't care.

## 3 — Structured logging via `log/slog`  **Win**

**BGIO flaw.** `console.log` everywhere; no structured fields, no
levels, no JSON output, no per-request correlation IDs.

**Go fix.** Stdlib `log/slog` (Go 1.21+). Manager and server log
match/move/connect events with structured fields (`match_id`,
`player_id`, `move`, `dur_ms`). Default handler is text for dev; users
can swap in a JSON handler for prod. Zero new deps.

## 4 — `expvar` + `net/http/pprof`  **Win**

**BGIO flaw.** No first-class observability. The Node.js server has no
built-in profiler; you bolt on `clinic.js` or restart with `--inspect`.

**Go fix.** Mount `net/http/pprof` and `expvar` on an opt-in admin route
(or a separate port). Free heap/CPU profiles, free
match-count / move-count expvar. Stdlib only.

## 5 — Prometheus `/metrics`  **Maybe**

**BGIO flaw.** No metrics surface. To monitor a BGIO server you instrument
externally.

**Go fix.** Prometheus client. Either pull
`prometheus/client_golang` (industry standard, ~5MB transitive) or roll a
minimal `/metrics` handler in stdlib (`expvar` already gets us most of
the way). The dep is heavy for a small framework — propose `expvar` as
the default and a `/metrics` Prometheus path as a separate package.

## 6 — Stricter `_stateID` semantics (mandatory, monotonic)  **Win**

**BGIO flaw.** BGIO's stateID is best-effort — clients can race ahead with
optimistic moves and the server may or may not reject mismatches. Our
current port has the same opt-in (`req.StateID > 0` skips check).

**Go fix.** Add an opt-in *Server* mode `Manager.RequireStateID = true`
that rejects any move whose request didn't supply a `StateID`. Production
servers turn it on; tests/dev leave it off. Backwards-compatible.

## 7 — Distributed scaling via shared storage + StateID  **Maybe**

**BGIO flaw.** BGIO requires sticky sessions because state lives in
per-process memory. Scaling out means socket.io + Redis adapter; lots of
moving parts.

**Go fix.** Because we already serialise per-match writes and ship a
`StateID` on every state, an optimistic-concurrency `Update` on a shared
SQL backend gives you a horizontal-scale server with no sticky-session
requirement. Two servers racing on the same match → one loses the CAS
and retries.

**Cost.** Real work — needs an `UpdateIfMatchesStateID` method on
`Storage`, a Postgres adapter, a Pub/Sub fan-out for broadcasts.
Probably out of scope for an "afternoon's worth of leverage" but a clear
direction.

## 8 — Determinism: forbid wall-clock & `math/rand` inside moves  **Maybe**

**BGIO flaw.** Nothing stops a move from calling `Date.now()` or
`Math.random()`, which breaks replay and AI rollouts.

**Go fix.** Engine-level: nothing we can enforce at runtime without
trapping syscalls. **But** we can ship a `go vet`-style analyser
(`cmd/boardgame-go-vet`) that flags `time.Now`, `rand.*` and similar
inside any `MoveFn`/`HookFn`. Cheap, valuable for game authors.

## 9 — Built-in chat/log rate limits  **Skip**

Real concern, but it's a generic web-server concern, not a BGIO-specific
flaw. Sit on the HTTP layer (`golang.org/x/time/rate` middleware) — no
need to bake into the framework.

## 10 — Native HTTP/2 & HTTP/3  **Skip**

Stdlib `net/http` already serves HTTP/2 over TLS. HTTP/3 would need
`golang.org/x/net/quic` — interesting but premature.

## 11 — First-class bots framework  **Maybe**

**BGIO has this.** RandomBot and MCTSBot. Not really a "Go fix" — it's
just parity work we deferred. Ship if/when we want.

## 12 — Builder pattern for game definitions  **Skip**

Tempting (`NewGame().WithMoves(...).WithTurn(...)`), but Go's struct
literals are already pretty good, and a builder forces every plugin
author to maintain a parallel builder API.

## 13 — Compile-time game validation  **Maybe**

**BGIO flaw.** Misconfigured games (`start: true` on two phases, `Next:`
pointing at a non-existent phase, etc.) fail at the first reproduction
of the bug.

**Go fix.** `func (g *Game) Validate() error` called at registration in
`Manager.Register`. Catches: duplicate `Start: true`, dangling `Next`
references, duplicate moves, unknown stages referenced in `setStage`.

## Recommended set to ship now

In priority order:

1. **Generic typed games** (#1). Biggest ergonomic win; new API alongside
   the existing one.
2. **`context.Context` propagation** (#2). API-touching but mechanical.
3. **`Game.Validate()` at registration** (#13). Cheap, catches real bugs.
4. **Structured logging via `slog`** (#3). Zero-cost, zero deps.
5. **`expvar` + `pprof` mount** (#4). Stdlib only.
6. **Mandatory-StateID server mode** (#6). Tiny, locks down a real race.

The rest (distributed scaling, Prometheus, lint, bots) are real but
either bigger projects or arguably extras — leaving them for follow-up
work.
