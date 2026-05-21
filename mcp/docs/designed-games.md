# Designing your own games

The boardgame-mcp server lets Claude help you design and play your own board games on the fly. Two entry points depending on what you're after:

- **Inventing a new game** → invoke the `design-a-game` prompt. Claude walks you through a structured interview (theme, players, moves, win condition, hidden info, randomness), drafts the Starlark spec inline as you answer, and registers it once you're aligned.
- **Implementing a real-world game** (chess, checkers, go, connect-4, mancala, dots-and-boxes, gomoku, …) → invoke the `implement-a-known-game` prompt. Claude skips the interview, one-shots the spec from canonical rules, playtests it against scripted scenarios, and registers. Faster path when the rules aren't a creative input.

Both prompts produce the same kind of artefact: a Starlark spec registered against your user via `register_game`, with the same on-disk layout in stdio mode.

## Design walkthrough (new games)

1. Open a Claude session connected to the boardgame-mcp server.
2. Invoke the `design-a-game` prompt. Claude reads the design guide and asks you about your game one question at a time: theme, players, moves, win condition, hidden info, randomness.
3. As you answer, Claude drafts a Starlark spec inline. When you're aligned on a draft, Claude calls `playtest_draft` to dry-run a few scenarios. Bugs surface here; you and Claude fix them in the conversation.
4. When the playtest looks right, Claude calls `register_game`. The game is persisted to your account and visible to your future Claude sessions via `list_games`.
5. Play the game like any built-in: `create_match` → `join_match` → take turns calling `make_move`.

## Implement walkthrough (known games)

1. Invoke the `implement-a-known-game` prompt. Say which game ("let's do chess") and any variant tweaks (FIDE rules, no castling, etc.).
2. Claude confirms scope in one or two questions, then writes the whole spec in one reply.
3. Claude calls `playtest_draft` with 3–5 scenarios covering opening, mid-game, an illegal-move attempt, and a winning move. Reads the trace; fixes bugs; re-playtests.
4. Once it looks right, Claude shows you the move list and calls `register_game`.
5. Play like any other registered game.

Engine limits to know about: every successful move ends the player's turn (no multi-action turns yet), no phases or stages, args are positional primitives. Games that need those features (Catan, Magic, Stratego's setup phase, trick-takers) need a reduced v1 or wait for the upcoming phase/stage support.

## Using engine modules (MODULES)

Declare engine modules your game uses with a top-level `MODULES` list and call
them through `ctx.modules.<name>.*` inside setup/apply:

    MODULES = ["ccg"]

    def setup(ctx):
        ctx.modules.ccg.new_zone(name="deck", ordered=True)
        ctx.modules.ccg.new_zone(name="hand", ordered=False)
        for i in range(40):
            c = ctx.modules.ccg.new_entity(type="card", owner="0")
            ctx.modules.ccg.move_to(entity=c, zone="deck")
        return {}

    def draw(state, ctx):
        top = ctx.modules.ccg.draw(zone="deck", n=1)
        ctx.modules.ccg.move_to(entity=top[0], zone="hand")
        return state

Entities are referenced by opaque tokens (e.g. `"ent:7"`); pass them back to
module ops, don't parse them. The `ccg` module ships zones, entities, and
`move_to`/`draw`/`size`/`members`.

### tabletop module (spatial games)

For board/grid games, declare `tabletop` and build a board in setup, then place
and move units by integer id on integer `(x, y)` coordinates:

    MODULES = ["tabletop"]

    def setup(ctx):
        ctx.modules.tabletop.new_board(kind="hex", w=10, h=10)  # or kind="square"
        ctx.modules.tabletop.place(unit=1, x=0, y=0)
        return {}

    def advance(state, ctx):
        x, y = ctx.modules.tabletop.position_of(unit=1)
        ns = ctx.modules.tabletop.neighbors(x=x, y=y)
        ctx.modules.tabletop.move(unit=1, x=ns[0][0], y=ns[0][1])
        return state

Ops: `new_board(kind, w, h)`, `place(unit, x, y)`, `move(unit, x, y)`,
`remove(unit)`, `position_of(unit)` → `[x, y]` or `None`, `entities_at(x, y)` →
`[unit, …]`, `within(x, y, radius)` → `[unit, …]`, `neighbors(x, y)` →
`[[x, y], …]`, `line_of_sight(fx, fy, tx, ty)` → bool (honors terrain tagged
`"blocks_los"`), `tag_terrain(x, y, tag)`, `has_terrain(x, y, tag)`. Units are
plain integer ids you choose; positions are plain coordinates. Dice/combat are
not yet bridged.

economy/shop, target selection, and event hooks land in later phases.

## Where games live

- Built-ins (tic-tac-toe, love-letter) come from the server binary.
- **Stdio mode (default):** each designed game lives as an on-disk skill at `$HOME/.claude/skills/games/<game-name>/`:
  - `SKILL.md` — auto-generated rich rendering (frontmatter + auto-rendered moves table + the designer's notes + strategy placeholder). Regenerated on every `register_game`; hand-edits to it are overwritten on the next save.
  - `spec.star` — canonical Starlark spec source.
  - `guide.md` — canonical `llm_guide` content (only present if the designer authored one).
  
  Hand-edit `spec.star` or `guide.md` between sessions; the SKILL.md is the auto-generated view. Override the root with `--skills-dir=PATH`; pass `--skills-dir=""` to keep games in memory only (ephemeral).
- **Hosted mode (`--database-url` set):** designed games go to the `user_games` Postgres table, scoped to the registering user. Use `export_game` to retrieve the skill-shaped artefact.

The server scans the skills directory on startup and registers every game it finds. v0.4–v0.5.1 stored a "skinny" SKILL.md (no auto-rendered moves table, llm_guide embedded as the body); those files still load (back-compat path extracts the llm_guide from the body), but the next `register_game` migrates them to the new three-file layout.

## Deleting a game

Tell Claude "delete the foo game" and it'll call `delete_game(name="foo")`. Built-ins are protected. Existing matches of a deleted game become unplayable but readable.

## Exporting a game for a per-game skill

`export_game(name="foo")` returns a skill-shaped package:

- `skill_md` — a SKILL.md skeleton with YAML frontmatter, an auto-rendered moves table, and the designer's `llm_guide` (if any). Strategy prose, UI notes, and AI heuristics are placeholders for the author to fill in.
- `spec_star` — the Starlark spec source.
- `manifest` — structured metadata (name, players, owner, moves with declared arg shapes, `created_at`).

The intended flow: write `skill_md` to `~/.claude/skills/<game>/SKILL.md` and `spec_star` to `~/.claude/skills/<game>/spec.star`, then layer in a `<game>/strategy.md`, a React UI, AI logic, etc. — anything the server can't auto-generate. In stdio mode the SKILL.md + spec.star are already on disk under `--skills-dir`; `export_game` is the way to get the same package out of hosted mode, and gives you a richer auto-generated SKILL.md than what the registration step writes.

Built-ins can't be exported (no Starlark source to ship). Cross-owner exports are refused.

## Limits

v1 supports any 2+ player turn-based game representable as a Starlark module. Out of scope: real-time games, simultaneous moves, multi-action turns (every successful move ends the turn), publishing designed games to other users.

## How Claude actually plays a designed game

When the active match's game is one you designed, Claude reads the `game://<your-id>/<name>/guide` MCP resource (if you wrote an `llm_guide` at registration time) before its first move. That guide carries your rules-and-strategy notes — write them with the LLM as the audience.
