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
- **Stdio mode (default):** each designed game lives as an on-disk skill at `$HOME/.claude/skills/games/<game-name>/`, containing a human-readable `SKILL.md` (frontmatter + your `llm_guide` markdown) and a `spec.star` (the Starlark spec the server runs). Override the root with `--skills-dir=PATH`; pass `--skills-dir=""` to keep games in memory only (ephemeral).
- **Hosted mode (`--database-url` set):** designed games go to the `user_games` Postgres table, scoped to the registering user.

You can browse, hand-edit, copy between machines, or back up your designed games like any other files. The server scans the skills directory on startup and registers every game it finds.

## Deleting a game

Tell Claude "delete the foo game" and it'll call `delete_game(name="foo")`. Built-ins are protected. Existing matches of a deleted game become unplayable but readable.

## Limits

v1 supports any 2+ player turn-based game representable as a Starlark module. Out of scope: real-time games, simultaneous moves, multi-action turns (every successful move ends the turn), publishing designed games to other users.

## How Claude actually plays a designed game

When the active match's game is one you designed, Claude reads the `game://<your-id>/<name>/guide` MCP resource (if you wrote an `llm_guide` at registration time) before its first move. That guide carries your rules-and-strategy notes — write them with the LLM as the audience.
