# Designing your own games

The boardgame-mcp server lets Claude help you design and play your own board games on the fly.

## Walkthrough

1. Open a Claude session connected to the boardgame-mcp server.
2. Invoke the `design-a-game` prompt. Claude reads the design guide and asks you about your game one question at a time: theme, players, moves, win condition, hidden info, randomness.
3. As you answer, Claude drafts a Starlark spec inline. When you're aligned on a draft, Claude calls `playtest_draft` to dry-run a few scenarios. Bugs surface here; you and Claude fix them in the conversation.
4. When the playtest looks right, Claude calls `register_game`. The game is persisted to your account and visible to your future Claude sessions via `list_games`.
5. Play the game like any built-in: `create_match` → `join_match` → take turns calling `make_move`.

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
