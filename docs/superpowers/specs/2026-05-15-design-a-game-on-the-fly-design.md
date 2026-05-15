# Design-a-Game-on-the-Fly — Design

**Date:** 2026-05-15
**Status:** Draft, pre-implementation-plan
**Owner:** Tyler Crandall
**Related:** [`mcp/`](../../../mcp/) plugin (#22–#26), [`core/`](../../../core/) reducer, [`mcp/ownership.go`](../../../mcp/ownership_pg.go)

## Goal

Let a Claude session and a human co-design a brand-new board game during a conversation, register the resulting spec on the boardgame-go MCP server, and play it through the same MCP tools (`create_match` / `join_match` / `make_move`) as a built-in game like tic-tac-toe.

The designed game must be a real, server-enforced game: deterministic reducer, hidden-info redaction, ownership scoping, persistence. It is **not** a Claude-narrated improv session.

## Non-goals

- Sharing or publishing designed games to other users. (Future feature; v1 is private per owner.)
- A graphical board renderer. The MCP boundary stays JSON; clients render however they want.
- Versioning / edit-after-play. v1: a designed game is immutable once registered. To revise, register a new one.
- Importing or porting games from other engines.
- Anything beyond two-or-more-player turn-based games. Real-time, simultaneous-action, and clocked games are out of scope.

## High-level architecture

```
┌──────────────────────── Claude session ────────────────────────┐
│                                                                │
│  1. invokes prompt:  design-a-game                             │
│     (server returns: design instructions + Starlark API ref)   │
│                                                                │
│  2. interactive design conversation with user                  │
│     ↓                                                          │
│  3. calls tool:      playtest_draft(spec, scenario)            │
│     (server runs spec in Starlark sandbox, returns trace)      │
│     ↓ (loop 2–3 as needed)                                     │
│                                                                │
│  4. calls tool:      register_game(spec, llm_guide)            │
│     (server validates, persists to user_games, returns id)     │
│                                                                │
│  5. normal play loop: list_games → create_match → make_move    │
│     (designed game is now indistinguishable from built-ins     │
│      for this user's sessions)                                 │
└────────────────────────────────────────────────────────────────┘
```

Two clean phases: **design** (new MCP surface) and **play** (existing MCP surface, with a new registry source under the hood).

## Spec format (Approach A: single Starlark module)

A game spec is one Starlark source string with required top-level conventions:

```python
META = {
    "name": "misere-tictactoe",
    "min_players": 2,
    "max_players": 2,
    "description": "Three in a row LOSES.",
}

def setup(ctx):
    return {"cells": [None] * 9}

def player_view(state, player_id):
    return state  # no hidden info

MOVES = {
    "click_cell": {
        "args": [{"name": "id", "type": "int", "min": 0, "max": 8}],
        "apply": lambda state, ctx, id: _click(state, ctx, id),
    },
}

def _click(state, ctx, id):
    if state["cells"][id] != None: fail("occupied")
    state["cells"][id] = ctx.player_id

def end_if(state, ctx):
    line = _three_in_a_row(state)
    if line:
        return {"winner": _other_player(line[0])}
    if None not in state["cells"]: return {"draw": True}
    return None

def legal_moves(state, ctx):
    return [{"name": "click_cell", "args": [i]}
            for i in range(9) if state["cells"][i] == None]
```

### Required top-level bindings

| Name | Kind | Purpose |
|------|------|---------|
| `META` | dict | Static metadata: `name`, `min_players`, `max_players`, `description`. |
| `setup(ctx)` | fn | Returns initial state dict. Called once per match. |
| `MOVES` | dict | Maps move name → `{args: [...], apply: fn}`. |
| `end_if(state, ctx)` | fn | Returns `{winner: ...}` / `{draw: True}` / `None`. |
| `legal_moves(state, ctx)` | fn | Returns list of `{name, args}`. Required for `list_legal_moves`. |
| `player_view(state, player_id)` | fn, optional | Redact hidden info for one player. Defaults to identity (no hidden info). |

### Bridged context (`ctx`)

The Starlark `ctx` object is a host-provided builtin. It exposes only:

- `ctx.player_id` — string; the seat invoking the current move (or `""` in `setup`).
- `ctx.num_players` — int.
- `ctx.random.range(n)` — int in `[0, n)`, drawn from the engine's seeded RNG via [`plugins/random`](../../../plugins/random).
- `ctx.random.shuffle(list)` — in-place, seeded.
- `ctx.random.choice(list)` — seeded.
- `ctx.log(msg)` — append a short string to the engine move log; never used for game logic.

Every successful move ends the turn. Multi-action turns (BGIO's "one move, draw, play, end-turn" pattern) are out of scope for v1.

There is **no** access to time, environment, network, filesystem, or Go-side state.

### State mutation contract

`setup` returns a fresh dict; `apply` mutates the state dict in place; `end_if` and `legal_moves` and `player_view` are **pure** — they must not mutate state. The host catches mutation in pure functions by passing frozen dicts (Starlark's `freeze` builtin).

## Starlark host

We embed [google/starlark-go](https://github.com/google/starlark-go) — the canonical Go implementation, used by Bazel, Buildkite, and many others.

### Sandboxing

- Disable Starlark `load()`, `print` redirected to engine log.
- Step counter via `thread.SetMaxExecutionSteps()`. Default ceiling: 100k steps per move call. Configurable per-deployment.
- Wall-clock timeout per move via `context.Context` cancellation in `thread.SetLocal("ctx", goCtx)` checked in a tick hook. Default: 250ms.
- Memory: Starlark values are bounded by step count and by capping output sizes (state dict serialized must fit a 256 KB envelope by default).
- No reflection into Go: `ctx` is the only bridge; everything else is pure Starlark values.

### Determinism

- All randomness flows through `ctx.random.*`, which uses the engine's [`plugins/random`](../../../plugins/random) splitmix64 PRNG, seeded per match.
- Starlark has no built-in time/random by default, which fits our needs.
- Dict iteration order is insertion-ordered in starlark-go, which is the property we need for reproducibility.

## Engine integration

A registered Starlark spec becomes a `core.Game` via a new adapter in `mcp/starlarkgame/` (lives in the MCP sub-module, not the core engine — keeps the core dependency-free).

```
type starlarkGame struct {
    source string         // the Starlark module text
    meta   gameMeta       // parsed once at registration
    // host is rebuilt per call; each call gets a fresh thread.
}

func (g *starlarkGame) Name() string         { return g.meta.Name }
func (g *starlarkGame) MinPlayers() int      { return g.meta.MinPlayers }
func (g *starlarkGame) MaxPlayers() int      { return g.meta.MaxPlayers }
func (g *starlarkGame) InitialState(...) any { /* call setup */ }
func (g *starlarkGame) Reducer(...) any      { /* dispatch via MOVES[name].apply */ }
func (g *starlarkGame) PlayerView(...) any   { /* call player_view */ }
func (g *starlarkGame) EndIf(...) any        { /* call end_if */ }
```

State is `map[string]any` end-to-end (Starlark dict ↔ JSON ↔ Go map). The engine already accepts opaque `any` state; no `core/` changes required.

### Registry composition

Today, `core.Registry` holds the built-in games. We introduce a wrapping `UserAwareRegistry` *in the MCP layer*:

```
type UserAwareRegistry struct {
    builtins  *core.Registry
    userStore UserGameStore  // backed by user_games table
}

func (r *UserAwareRegistry) Lookup(userID, name string) (core.Game, error)
func (r *UserAwareRegistry) List(userID string) []GameInfo
```

`list_games` calls `List(userID)`; `create_match` calls `Lookup(userID, name)`. Built-ins win on name collisions (Claude can't shadow `tic-tac-toe`).

## The `list_legal_moves` problem

For built-in Go games we enumerate legal moves via the game's `EnumerateLegalMoves` method. For a Starlark game, enumeration must run inside Starlark.

**Resolution:** designed games are required to export `legal_moves(state, ctx)` returning a complete list. The MCP `list_legal_moves` tool calls this function. This shifts the enumeration burden to the spec author (Claude), which is the right place — Claude is already designing the move space and can describe it precisely.

If `legal_moves` is missing or too expensive, registration is rejected. The playtest tool exercises this during design, so failures surface before publication.

## MCP surface additions

### 1 prompt — `design-a-game`

Loads design-time instructions: the Starlark API reference, a worked example (misère tic-tac-toe), the design conversation script (ask the user about: game name, players, board/state shape, legal moves, win condition, hidden info needs), and a checklist for completeness. Output: Claude has everything it needs to author a spec collaboratively.

### 2 tools

#### `playtest_draft(spec, scenario)`

Runs a draft spec in the sandbox without registering it. `scenario` is an optional sequence of moves `[{player_id, move, args}, ...]`. Returns:

- `validation_errors`: `[]` if structural validation passes.
- `setup_state`: the initial state, or an error.
- `trace`: for each scenario step, `{state_before, state_after, end_if_result, error}`.
- `legal_moves_at_each_step`: result of calling `legal_moves` between scenario steps.

Idempotent and side-effect-free. No DB write. Bounded by the same step/time caps as live play.

#### `register_game(spec, llm_guide?)`

Validates spec, runs `setup` to ensure it doesn't crash, runs `legal_moves` on the initial state, then writes to `user_games`. Returns the registered game id. `llm_guide` is an optional markdown string (the "skill" content) stored alongside the spec and exposed as an MCP resource.

### 1 resource pattern — `game://<owner>/<name>/guide`

If a designed game has an `llm_guide`, the MCP server exposes it as a readable resource. Future Claude sessions, before playing a designed game, can read this resource to learn the rules and strategy hints the original designer encoded. Replaces the "ship a SKILL.md file" pattern for designed games, without requiring filesystem access.

The `boardgame-player` skill is extended with a one-liner: "If a match's game has a `game://.../guide` resource, read it before your first move."

## Persistence

New Postgres table, added to the same migration path as `match_ownership`:

```sql
CREATE TABLE user_games (
    user_id    TEXT NOT NULL,
    name       TEXT NOT NULL,
    spec       TEXT NOT NULL,          -- the Starlark source
    llm_guide  TEXT,                   -- optional markdown
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, name)
);
CREATE INDEX user_games_by_user ON user_games(user_id);
```

In stdio mode (no Postgres) the existing in-memory store gains a parallel `inMemoryUserGameStore`. Same interface, same semantics, lost on restart — which is fine for the single-user local case.

## Validation

Validation runs in this order at registration; first failure aborts:

1. **Parse**: source compiles as Starlark.
2. **Convention check**: `META`, `setup`, `MOVES`, `end_if`, `legal_moves` exist with correct types/signatures.
3. **META check**: name is unique per user, 1 ≤ min_players ≤ max_players ≤ 16, name matches `^[a-z0-9-]{1,40}$`.
4. **Setup smoke**: call `setup(ctx)` for `min_players` and `max_players`; result must be a dict that JSON-serializes.
5. **End-state smoke**: call `end_if(initial_state, ctx)`; must return `None` (otherwise the game would be over at start).
6. **Legal-moves smoke**: call `legal_moves(initial_state, ctx)`; must return a list, each entry shaped `{name, args}` where `name` is in `MOVES` and `args` matches the declared schema.

Validation runs entirely in the sandbox under the same caps as live play.

## Error handling

| Phase | Error class | Behavior |
|-------|-------------|----------|
| `playtest_draft` | Any spec error | Returned as structured field in the tool result. Tool itself succeeds. |
| `register_game` | Validation failure | Tool returns error with the specific failing check. No DB write. |
| `make_move` (live) | Starlark `fail(msg)` | Surfaced as a normal move-rejection error, like a built-in game's "occupied cell" error. |
| `make_move` (live) | Step/time cap exceeded | Move is rejected with an explicit "spec is too expensive" error. The match is effectively wedged: subsequent moves keep failing. `get_state` still works for forensics. Owner can delete the offending game (a new `delete_game` admin tool); existing matches of it become unplayable but readable. |
| `make_move` (live) | Starlark panic / host bug | Returned as a 500; match left intact. Logged for debugging. |

## Security

- Starlark sandbox: no `load`, no I/O, no Go reflection, bounded steps + wall time + memory.
- Specs are stored as text; no `eval` of stored data outside the sandbox.
- Spec source is treated as untrusted user content. The server never logs it at info level by default (it can be verbose / PII-adjacent / contain test data).
- Resource fetch (`game://.../guide`) is scoped to the requesting user's owned games or to public-built-in guides.
- Rate limits: registering ≥ 10 games per user per hour, or playtesting ≥ 60 times per minute, returns 429. Defaults configurable.

## Testing strategy

- **Unit (Starlark host)**: `mcp/starlarkgame/` — parse, validate, setup, apply, end_if, legal_moves, player_view; step/time caps; mutation-in-pure-function detection; RNG determinism (same seed → same trace).
- **Unit (registry)**: `UserAwareRegistry` — builtin priority over user games, scoping, list filtering.
- **Unit (storage)**: `user_games` table — in-memory and Postgres stores tested with the same suite (existing pattern in `ownership_test.go`).
- **Integration (MCP)**: `prompts/get design-a-game` returns expected content; `playtest_draft` with good and bad specs; `register_game` end-to-end; `list_games` shows registered game; full play loop on a registered game.
- **End-to-end ("dogfood")**: a CI test that includes a known-good designed-game spec (e.g. misère tic-tac-toe), registers it, plays a scripted match to completion, and asserts the winner.
- **Determinism check**: run the same designed match twice with the same seed; states must match byte-for-byte at every step.

## Open questions (carry into plan)

- **Move arguments richer than ints**: do we need typed dict args, or are positional primitives + the args schema enough for v1? (Tentatively: positional primitives only — keeps `list_legal_moves` payloads simple.)
- **Phases / stages**: BGIO's phase/turn/stage machinery is rich. Do designed games need it, or is the implicit "every move ends a turn unless `ctx.turn.end()` says otherwise" enough? (Tentatively: implicit only; phases are a v2 feature.)
- **Multi-round games**: deferred. Loveletter-style "first to N points across rounds" can be approximated in v1 by treating the whole campaign as one long game; explicit rounds are v2.
- **Spec size cap**: pick a number. (Tentatively: 64 KB Starlark source per spec.)
- **`legal_moves` performance cap**: if a game has 10^4 legal moves, the response is huge. Cap at ~1000 returned moves? Truncate with a marker? (Tentatively: hard-fail at 1000; force the designer to reduce.)

## Out of scope (explicitly)

- Game sharing / publication / discovery beyond the owning user.
- Editing a registered game (re-register under a new name instead).
- Spectator mode for designed games (inherits whatever `core` supports — no special handling).
- Visual board renderer / web UI for design conversations.
- Importing rules from prose (one-shot "design from this description"). The structured design conversation always runs.

## Surface change summary

| Area | Change |
|------|--------|
| `core/` | None. |
| `mcp/starlarkgame/` (new) | Starlark host, validation, `core.Game` adapter. |
| `mcp/usergames.go` (new) | `UserGameStore` interface + in-memory impl. |
| `mcp/usergames_pg.go` (new) | Postgres impl with migration for `user_games`. |
| `mcp/registry.go` (new) | `UserAwareRegistry` wrapping `core.Registry`. |
| `mcp/tools.go` | Add `playtest_draft`, `register_game`, `delete_game`; existing tools route through `UserAwareRegistry`. |
| `mcp/prompts.go` | Add `design-a-game` prompt. |
| `mcp/server.go` | Wire `game://` resource handler. |
| `mcp/skills/boardgame-player/SKILL.md` | One-line addendum about reading the `game://.../guide` resource. |
| Tests | New `_test.go` files alongside each new file; integration tests in `mcp/`. |
| Docs | `mcp/docs/designed-games.md` (user-facing guide), this spec lives here for reference. |

Root module deps unchanged. New dep `go.starlark.net` lives in `mcp/go.mod`.
