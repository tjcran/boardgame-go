The user named a real-world board game (chess, checkers, go, connect-4, mancala, dots-and-boxes, gomoku, isolation, hex, reversi, nim, …). You know its canonical rules from your training data. Your job is to one-shot a Starlark spec for boardgame-mcp, playtest it for bugs, and register it. Skip the design interview entirely — for known games the rules aren't a creative input, they're a finished thing.

# The Starlark contract (same as design-a-game)

A spec is one Starlark module with these required top-level names. Keep this section in sync with `design_a_game.md` — both prompts target the same engine.

| Name | Kind | Purpose |
|------|------|---------|
| `META` | dict | `{"name": "...", "min_players": N, "max_players": M, "description": "..."}`. `name` matches `^[a-z0-9-]{1,40}$`. |
| `setup(ctx)` | function | Returns the initial state as a dict. |
| `MOVES` | dict | `{"move_name": {"args": [{"name":"x","type":"int","min":0,"max":N}], "apply": fn, "ends_turn": True}}`. `apply(state, ctx, *args)` MUST `return` the new state dict — input `state` is frozen, so mutations raise. Use `fail("...")` to reject a move. `"ends_turn": False` keeps the same player active (multi-action turns, e.g. Catan's roll-then-build); defaults to `True`. |
| `end_if(state, ctx)` | function | Returns `{"winner": "0"}` / `{"draw": True}` / `None`. |
| `legal_moves(state, ctx)` | function | Returns list of `{"move": ..., "args": [...]}` — same shape the server returns. |
| `player_view(state, player_id)` | function (optional) | Redact hidden info for one player. Default: identity. |
| `PHASES` | dict (optional) | `{"phase_name": {"moves": {...}, "end_if": fn, "start": True}}`. Phase-scoped move tables; the phase's `end_if(state, ctx)` returns a phase-name string to transition or `None` to stay. Exactly one phase sets `"start": True`. Use for Stratego's place-then-play, trick-takers' bid-then-play, distinct opening/midgame/endgame phases when rules genuinely differ. Skip PHASES for single-flow games like chess or checkers. |
| `STAGES` | dict (optional) | `{"stage_name": {"moves": {...}, "next": "other_stage"}}`. Sub-modes within a turn. From an apply function call `ctx.events.set_stage("stage_name")` to gate the current player into the stage (only the stage's moves are legal for them); call `ctx.events.end_stage()` to leave. Mark the entering move `"ends_turn": False`. Use for loveletter Prince ("play card → pick a target → resolve"), "discard to N" gates, Magic-style targeted spell resolution. |

`ctx` exposes `ctx.player_id`, `ctx.num_players`, `ctx.phase`, `ctx.random.range(n)` / `.shuffle(list)` / `.choice(list)` (seeded), `ctx.log(msg)`, and `ctx.events.set_stage(name)` / `ctx.events.end_stage()` (apply only). No time, no I/O, no filesystem, no network. All five functions are pure: copy lists/dicts before changing them — `new_cells = list(state["cells"]); new_cells[i] = X` then return `{"cells": new_cells, ...}`.

**Current engine limits** (matter for known games):
- Multi-action turns: per-move `"ends_turn": False` (default `True`). Catan-style roll-then-build, MTG-style draw-then-play work — mark non-terminal moves `False`.
- Phases: declare a `PHASES` dict with scoped move tables and `end_if` transitions. Use for setup-then-play, bid-then-play, or markets-then-build games.
- Stages: declare a `STAGES` dict; enter via `ctx.events.set_stage("name")` from apply, leave via `ctx.events.end_stage()`. Covers loveletter-Prince-style "now pick a target" patterns.
- Args are positional primitives (int / string / bool). No nested dicts in args.

If the game still doesn't fit (e.g. simultaneous actions, real-time clocks), tell the user upfront and offer a reduction. Don't silently downgrade.

# Your workflow

1. **Confirm the game and any variant.** "Chess with standard FIDE rules? With or without castling / en passant?" Most users want the full thing; some want a kid-mode variant. Don't ask more than one or two questions — you're not designing, you're confirming scope.
2. **Write the spec in one go.** Inline the full Starlark module in your reply. Treat it as production code: include the wrap-around logic, the win-condition enumeration, the legal-moves filter (e.g., chess: pseudo-legal moves filtered by "doesn't leave my king in check"). Use named helper functions (`def _attacks(...)`) liberally — Starlark doesn't have classes but does have plain functions. Keep functions under ~30 lines each where you can; the step cap is 100k per call and complex evaluators eat steps fast.
3. **Playtest with 3–5 scripted scenarios.** Call `playtest_draft` with the source and a `scenario` array that covers (a) the opening move, (b) a mid-game move that's borderline legal, (c) an illegal move attempt that should be rejected by `fail`, (d) a move that ends the game (capture, mate, three-in-a-row, etc.). Read the returned trace — the `state_after` and `end_if_result` per step tell you whether the spec is wrong.
4. **Fix bugs and re-playtest** until the trace looks right. Common issues: off-by-one on board indexing, forgetting to copy a list before mutating it (frozen input raises), `legal_moves` returning the wrong dict key (use `"move"`, not `"name"` for the move name), `end_if` triggering at setup.
5. **Confirm with the user, then `register_game`.** Show them the move list and any variant choices you made. When they OK it, call `register_game(source=<...>, llm_guide=<short markdown explaining notable strategy hints or rule clarifications>)`. The `llm_guide` becomes future Claude sessions' reference when playing your game — write it for the LLM audience.

# Worked example: connect-4

```python
META = {
    "name": "connect-4",
    "min_players": 2, "max_players": 2,
    "description": "Drop a disc into a column; first to four in a row (horizontal, vertical, or diagonal) wins.",
}

ROWS = 6
COLS = 7

def setup(ctx):
    return {"cells": [None] * (ROWS * COLS)}

def _column_top(cells, col):
    # Returns the row index where the next disc lands, or -1 if full.
    for r in range(ROWS - 1, -1, -1):
        if cells[r * COLS + col] == None:
            return r
    return -1

def _drop(state, ctx, col):
    row = _column_top(state["cells"], col)
    if row < 0:
        fail("column full")
    new_cells = list(state["cells"])
    new_cells[row * COLS + col] = ctx.player_id
    return {"cells": new_cells}

MOVES = {
    "drop": {"args": [{"name": "col", "type": "int", "min": 0, "max": 6}], "apply": _drop},
}

def _four_starting_at(cells, r, c, dr, dc):
    v = cells[r * COLS + c]
    if v == None: return None
    for k in range(1, 4):
        nr = r + dr * k
        nc = c + dc * k
        if nr < 0 or nr >= ROWS or nc < 0 or nc >= COLS:
            return None
        if cells[nr * COLS + nc] != v:
            return None
    return v

def end_if(state, ctx):
    for r in range(ROWS):
        for c in range(COLS):
            for dr, dc in [(0, 1), (1, 0), (1, 1), (1, -1)]:
                w = _four_starting_at(state["cells"], r, c, dr, dc)
                if w != None:
                    return {"winner": w}
    if None not in state["cells"]:
        return {"draw": True}
    return None

def legal_moves(state, ctx):
    return [{"move": "drop", "args": [c]} for c in range(COLS) if _column_top(state["cells"], c) >= 0]
```

That's ~50 lines. Chess is ~500. Either way the pattern is the same: one module, helper functions, return new state, use `fail` for illegal moves.

# Common pitfalls (read these before writing)

- **Forgetting to return.** Every `apply` must return a new state dict. Returning `None` errors with "apply returned None". `fail("...")` is the only way to reject.
- **Mutating frozen inputs.** `state["cells"][i] = X` raises. Always `new = list(state["cells"]); new[i] = X; return {"cells": new}`.
- **Wrong `legal_moves` key.** Each entry is `{"move": ..., "args": [...]}`. The `"name"` spelling is also accepted but `"move"` matches what the response shows.
- **Off-by-one board indexing.** Sanity-check by hand: state `[None]*9` at setup, indices 0..8, etc.
- **`end_if` triggers at setup.** Validation will reject this. Make sure the initial state doesn't already satisfy any end condition.
- **`legal_moves` enumerates too many.** If a move has 10⁴ legal arg combinations, the response is huge. Decompose into smaller moves (`select_piece` then `select_destination`) or filter aggressively in your enumeration.

# After registration

The game is in `list_games` immediately. Your future sessions read the `game://<user>/<name>/guide` MCP resource (the `llm_guide` you wrote) before playing. The Starlark spec and SKILL.md live at `~/.claude/skills/games/<name>/` in stdio mode — the user can hand-edit `spec.star` to tweak rules and the change takes effect on the next server start (or after a re-`register_game`).
