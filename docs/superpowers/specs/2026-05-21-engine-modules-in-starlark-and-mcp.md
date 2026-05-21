# Exposing engine modules to Starlark + MCP (design)

Date: 2026-05-21
Status: design — pending implementation plan

## Problem

Designing and playing **complex** games through the boardgame-mcp server is
blocked by Starlark vocabulary, not by Starlark the language. Today a designed
game's spec (`mcp/starlarkgame/`) can only reach a minimal `ctx`: it has META,
MOVES, setup, end_if, legal_moves, player_view, PHASES, STAGES, and per-move
`ends_turn`. Args are positional primitives (`int`/`string`/`bool`). `load()` is
disabled, so specs cannot share helper libraries.

The consequence: any game with decks, hands, hidden info, board geometry,
resource pools, triggered abilities, or mid-move target selection has to
**re-implement those primitives inside the Starlark spec**, encoded into
integers. That balloons specs, burns chat context on plumbing instead of rules,
and is error-prone.

Meanwhile the engine **already has** battle-tested Go implementations of exactly
these primitives, used by built-in games:

| Module | Provides |
|---|---|
| `modules/ccg` | Entities + zones (hand/deck/discard/battlefield), modifiers/auras with layered recalc, in-process event bus for triggers, target queries, the **Effect envelope** (`PendingEffects` queue + `ResolveNext` + lifecycle events for counterspells/redirection/replacement) |
| `modules/tabletop` | Hex grid, square grid, positions, terrain, basic combat, line-of-sight/distance |
| `modules/economy` | Resource pools, scaling |
| `modules/shop` | Buy / freeze / refresh |
| `core/target.go` | `TargetRequest` block/resume protocol |
| `core/events.go`, `core/phase.go`, `core/stage.go`, `core/playerview.go` | Events, phases, stages, hidden info |
| `plugins/random`, `plugins/player` | Seeded RNG, per-player state |

**The gap is purely that the Starlark sandbox cannot reach any of it.**

## Goal

Bridge the existing engine modules into two surfaces from one shared binding
layer:

1. **Runtime (Starlark callables):** registered game specs call engine modules
   from inside their move `apply` functions via a curated `load()` whitelist.
2. **Design-time (MCP tools):** Claude calls the same module operations
   interactively while prototyping a draft game — an interactive REPL on a live
   draft match — tightening the design/playtest/debug loop.

Both surfaces are generated from one Go registry, so design-time and runtime
mechanics cannot diverge.

### Non-goals

- Building a parallel "game kit." Everything here wraps existing `modules/*`;
  no new game primitives are invented.
- A declarative card/effect DSL (rules-as-data). Explicitly deferred — revisit
  only after real games reveal repeated patterns.
- Fixing game classes the modules don't address: real-time games, simultaneous
  decisions (7 Wonders draft, Diplomacy orders), drafting-with-passing, or
  content-volume limits (a 200-card set still won't fit in one chat reply).
  Those are separate gaps, not Starlark-vocabulary gaps.
- Runtime "Claude-as-referee" (the rejected D2): players use the normal
  `make_move` path; designed games stay deterministic and replayable.

## Architecture

```
   modules/ccg            modules/tabletop        modules/economy/shop
        \                       |                          /
         +--------- mcp/modulebridge/  (Go layer) ---------+
                       /                       \
        Starlark callables                MCP tools
        (load("modules/ccg.star"))        (ccg_push_effect, …)
              ↑                                   ↑
   used by registered specs            used by Claude at design-time
   at match-runtime                    (REPL on a draft match)
```

`mcp/modulebridge/` is the single source of truth. One Go function per engine
operation, wrapped twice (Starlark callable + MCP tool) by iterating a shared
registry. It owns the opaque-handle table, argument marshalling, and resolver
registration. It lives inside the MCP go.mod (it depends on `modules/*`, which
the root engine never imports).

### Module state lives in G, not in the state dict

Critical impedance mismatch the engine resolves: the Starlark adapter today uses
a **pure `map[string]any` as `G`** — every engine call (`CallSetup`/`CallMove`/…
in `call.go`) takes a frozen state dict and returns a new one; `apply` is a pure
transform. But `modules/ccg` and friends work by **mutating a `*ccg.State` Go
struct** with unexported, unserialized monotonic ID counters
(`State.nextEntityID`, …). Serializing module state into the dict and rebuilding
it each call would reset those counters and collide IDs.

Resolution: designed games get a real struct `G`, not a bare dict:

```go
// mcp/starlarkgame/game.go
type StarlarkG struct {
    Data    map[string]any // user game state (what apply returns)
    Modules map[string]any // live module states: "ccg" -> *ccg.State, …
}
```

`Data` is the dict the spec's `apply` reads and returns (unchanged contract from
the spec author's view). `Modules` holds the live Go module states, instantiated
once in `Setup` per the `MODULES` declaration and carried in memory across moves
exactly like a native Go game's `G`. `apply` never sees `Modules` as data — it
reaches module operations only through `ctx.modules.<name>.*` bindings, which
mutate the live structs. Counters persist because the structs persist.

Consequences:
- `game.go` closures change from `mc.G.(map[string]any)` to `mc.G.(*StarlarkG)`,
  passing `.Data` to the `Call*` functions and reattaching `.Modules`.
- `BridgeCtx` carries the module states so `ctx.modules.*` bindings can mutate
  them; the input `Data` dict stays frozen (module mutations are Go-side, not
  dict writes, so freezing is not violated).
- **Replay** is correct: replay re-runs from `Setup`, rebuilding module states
  deterministically. **Storage round-trip** (serializing `G` to disk between
  moves) drops the unexported counters — but this is a *pre-existing* `ccg`
  limitation that equally affects native Go games using `ccg`, so it is
  consistent and out of scope here. The in-memory `Manager` holds the live `G`
  across moves, so live play is unaffected.

## Components

### New: `mcp/modulebridge/`

- **`registry.go`** — the shared table. Each entry:
  ```go
  type Op struct {
      Name          string          // "ccg.push_effect"
      Module        string          // "ccg"
      GoFn          BridgeFn        // the actual call into modules/*
      ArgSchema     []ArgSpec       // validated before GoFn runs
      MCPTool       string          // "ccg_push_effect" (design-time)
      Deterministic bool            // must be true to be registerable
  }
  ```
  The Starlark builder and the MCP tool registry both iterate this table. Add a
  row → get both surfaces. Non-deterministic ops are not registerable (see
  Determinism guard).
- **`handles.go`** — per-match opaque-handle registry. Maps Go typed values
  (`ccg.EntityID`, `tabletop.HexPos`, `ccg.EffectID`) to stable string tokens
  (`"ent:7"`, `"hex:2,3"`). Starlark and MCP both see tokens; Go side decodes
  back to typed values. Tokens are derived **deterministically from the
  underlying entity ID / coordinate** (not an allocation counter) so the table
  is reconstructable from persisted state in hosted mode.
- **`ccg.go`** — binds `modules/ccg`: `new_zone`, `move_to`, `push_effect`,
  `resolve_next`, `subscribe`, `publish`, `set_visibility`, `targets_in_zone`,
  modifier ops.
- **`tabletop.go`** — binds `modules/tabletop`: hex/square constructors,
  `neighbors`, `distance`, `line_of_sight`, terrain queries, basic combat.
- **`economy.go` / `shop.go`** — pools, scaling, buy/freeze/refresh.
- **`target.go`** — bridges `core.TargetRequest` into Starlark as a block/resume
  pair (see Data flow).

### Changed: `mcp/starlarkgame/` (additive, backward compatible)

- **`bridge.go`** — module bindings are exposed as `ctx.modules.<name>.<op>()`,
  matching the existing `ctx.events` / `ctx.random` sub-struct pattern. The
  `ctx.modules` struct is built from `modulebridge.Registry`, with each op's
  builtin closure backed by the live module state in `StarlarkG.Modules`. (No
  synthetic `load()` files — `load()` stays disabled. `ctx.modules` is simpler
  and the module state is already reachable via `BridgeCtx`.)
- **`spec.go`** — new top-level `MODULES = ["ccg", "tabletop.hex", "economy"]`.
  `Setup` instantiates the corresponding Go state (`ccg.NewState()`, …) into
  `StarlarkG.Modules` keyed by module name (see "Module state lives in G").
- **Args schema** extended with semantic types: `{"type": "entity", "zone":
  "hand"}`, `{"type": "hex"}`, `{"type": "target"}`. The engine validates these
  against module state before `apply` runs. Existing primitive args unchanged.
- **`HOOKS`** — new optional top-level dict, e.g. `HOOKS = {"on_event": fn}`,
  registered on the relevant module's event bus during setup.

### Changed: `mcp/tools.go`

- New module-tool group registered from `modulebridge.Registry`
  (`ccg_push_effect`, `tabletop_hex_neighbors`, …). Match-scoped: every call
  takes `matchID` and is ownership-checked.
- **Design-time only**, flag-gated: exposed only while designing/playtesting a
  draft game. After `register_game`, players use `make_move`; the module tools
  are not the play path.

### Unchanged

- `core/*`, `modules/*` themselves — pure consumers, no edits.
- The `match.Manager` write path — module ops are called from inside an existing
  reducer/apply or (design-time) inside a normal match write, not as new
  top-level engine entry points.

## Data flow

### Design-time (Claude prototyping)

```
Claude → create_match(game="__draft__", spec=<starlark source>)
  → LoadSpec → host instantiates module state per MODULES
Claude → ccg_push_effect(matchID, kind="damage", target="ent:3", amount=3)
  → tools.go dispatches via modulebridge.Registry
  → modulebridge.ccg.PushEffect(*ccg.State, decoded args)  // direct Go call
  → state mutates inside the normal OCC-locked match write; events fire
Claude → get_state / playtest_draft → reads result, iterates
```

The draft is a real match (same Manager, locking, persistence). The only
difference: low-level module ops are callable on a draft instead of going
through `make_move`.

### Runtime (player plays a registered game)

```
Player → make_move(matchID, playerID, moveName, args)
  → Manager.MoveReqCtx → reducer → spec move.apply(state, ctx, args)
  → inside apply, Starlark calls ccg.push_effect(...)
  → modulebridge Starlark binding → modules/ccg  // SAME Go fn as design-time
  → apply returns; engine commits one StateID bump; subscribers broadcast
```

Same Go call, reached via Starlark callable instead of MCP tool. No design vs
runtime divergence in mechanics.

### TargetRequest (mid-move selection) — block/resume pair

`core.TargetRequest` is **not** an inline coroutine. The model (per
`core/target.go`): a move pushes a `BlockSpec` carrying a `TargetRequest`; the
cascade pauses; a **resume move** reads `mc.ResumingBlock.Target`, validates the
player's args with `core.ValidateSelection`, and applies the choice.

The Starlark binding mirrors this as a pair, not a magic inline return:

```
move "cast_bolt" apply:
    ctx.block_for_target({          # pushes BlockSpec.Target = TargetRequest
        "kind": "creature",
        "candidates": ctx.modules.ccg.targets_in_zone("battlefield"),
        "min": 1, "max": 1,
    })
    # apply returns here; cascade is paused

move "cast_bolt_resume" (resume):   # runs when player submits selection
    sel = ctx.resuming_selection()  # reads mc.ResumingBlock.Target + validates
    ctx.modules.ccg.push_effect({"kind": "damage", "target": sel[0], "amount": 3})
```

Candidates cross the bridge as opaque handle tokens; `ValidateSelection`'s
numeric-normalisation already tolerates the JSON float64/int gap for non-handle
candidates. Design-time, Claude drives the same flow via a `submit_target` MCP
tool.

### Persistence ("permanent skill")

`register_game(spec, llm_guide)` is unchanged. The spec's `MODULES = [...]`
declaration is the only new state, and it's self-contained: loading the skill on
a fresh match re-instantiates the named modules. No external state to capture.

## Error handling — the bridge boundary is the trust boundary

- **Invalid handle** (`"ent:99"` not in this match's table) → typed
  `ErrUnknownHandle`, surfaced as a clean Starlark/MCP error; move rejected,
  match state untouched; never panics, never mutates on a bad handle.
- **Type/arg mismatch** → rejected by `ArgSchema` validation *before* the Go
  call, with move name + arg name in the message.
- **Module not declared** (spec calls `ccg.*` without `"ccg"` in `MODULES`) →
  load-time error from a static scan in `LoadSpec`/`Validate`, not a runtime
  crash.
- **Resolver error** (a Starlark resolver callable raises) → propagated as
  effect-resolution failure; `ResolveNext` returns the error, `apply` decides
  abort-or-swallow; engine commits atomically or not at all.
- **TargetRequest abandonment** (selection never submitted) → existing match
  timeout/janitor reaps the paused cascade; partial state rolled back. (Confirm
  the block uses the staging the #56 work expects — see open questions.)
- **Runaway** → module calls count against the existing Starlark execution-step
  budget; no unbounded `push_effect` loop can wedge a match.

### Determinism guard

Every registered Op must be deterministic given (state, args, seeded RNG). The
bridge refuses to register any op that reads wall-clock or unseeded randomness;
`ccg` shuffles route through the seeded `plugins/random`. This is what keeps
`core.Replay` valid for designed games. Enforced via `Op.Deterministic` —
non-deterministic ops are simply not registerable.

## Testing strategy

1. **Bridge unit tests** (`modulebridge/*_test.go`) — table-driven off the
   registry so every Op is forced to have encode→call→decode coverage plus the
   error cases above.
2. **Dual-surface parity tests** — assert the Starlark-callable path and the
   MCP-tool path for the same Op produce identical state mutations. Guarantees
   design-time and runtime cannot diverge.
3. **Round-trip / replay tests** — design a mini-CCG and a hex skirmish, play a
   scripted sequence, `core.Replay` the log, assert byte-identical final state.
   Proves end-to-end determinism.
4. **Golden complex-game e2e** (`starlarkgame/e2e_test.go`) — one realistic spec
   per module (stripped Catan for tabletop+economy, a 6-card MtG-like for ccg)
   through the full `create_match → moves → end` path.
5. **Validate-time static checks** — MODULES-vs-references consistency and
   arg-schema well-formedness, with a test per rejection.

## What this unblocks

Moves the achievable ceiling from "abstract games + simple card games" to
~80% of mainstream board/card games: Catan-shaped (hex + resources + trades),
Magic/Hearthstone-shaped (stack + triggers + zones), Stratego/fog-of-war,
deckbuilders, worker-placement, co-op-with-scripted-events, trick-takers. The
remaining ~20% (real-time, simultaneous, drafting-with-passing, large content
sets) is out of scope here and tracked as separate gaps.

## Open questions (carry into the plan)

1. Exact suspend/resume staging contract with the #56 TargetRequest work — the
   resume-move binding (`block_for_target` / `resuming_selection`) must match
   how `Queue.Block` stages partial state and how the janitor reaps abandoned
   blocks.
2. Draft-only gating mechanism for the D1 module tools: a per-match `__draft__`
   flag vs. a separate MCP server mode.
3. Handle-table reconstruction — resolved by "module state lives in G": handles
   are deterministic tokens derived from the underlying entity ID / coordinate
   (`ent:7`, `hex:2,3`), so the table is a pure function of the live module
   state and needs no separate persistence. Confirm during implementation that
   every handle type has such a stable derivation.

## Implementation phasing

Built as a sequence of plans; each produces working, tested software:

1. **Core bridge + ccg** (this plan): `StarlarkG`, the `modulebridge` registry +
   handle table, the `MODULES`/`load()` wiring, the full `ccg` binding, the dual
   MCP-tool surface, determinism + replay tests. ccg is chosen first because it
   exercises every hard part (handles, live state across moves, dual surface,
   determinism, events).
2. **tabletop** binding (hex/square/terrain/combat) — reuses the mechanism.
3. **economy + shop** bindings — reuse the mechanism.
4. **TargetRequest** block/resume bindings + `submit_target` design-time tool.
5. **HOOKS** event-bus registration + semantic arg schema types.
```
