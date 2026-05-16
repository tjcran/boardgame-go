---
name: boardgame-player
description: Use when the user asks to play a board game against you via the boardgame-mcp server, or wants help running a match. Covers the protocol you must follow when calling the boardgame-mcp tools.
---

# Playing a board game against the user via boardgame-mcp

This skill activates when the `boardgame-mcp` server is connected and the user wants to play a game. It covers the tool-call protocol; per-game strategy lives in sibling skills (e.g. `tictactoe-strategy`).

## Available tools

| Tool | When to call |
|------|--------------|
| `list_games` | The user asks what games are available, or you don't know the exact game name. |
| `create_match` | Starting a new game. Always pass `numPlayers` matching the game's `MinPlayers`/`MaxPlayers`. |
| `join_match` | Always called after `create_match`. Save the returned `playerID` and `credentials` — every move requires them. |
| `get_state` | Between every move to see whose turn it is, the board state, and `gameover` info. |
| `list_legal_moves` | **Before every `make_move`.** Never guess move arguments. |
| `make_move` | Submit a move. Use exactly the `move` name and `args` from `list_legal_moves`. |

## The loop

```
1. list_games                        (once, if needed)
2. create_match                      (start)
3. join_match                        (claim your seat)
4. LOOP:
   a. get_state
   b. If state.gameover != null     → announce result, exit
   c. If state.currentPlayer == me  → list_legal_moves → pick one → make_move
   d. Else                          → wait for human; ask them to make their move
```

## Hard rules

- **Never invent move arguments.** Only use what `list_legal_moves` returned. The server's error messages call this out specifically.
- **Never `make_move` when it isn't your turn.** The server will reject with "not your turn" — and you'll have wasted a turn for the user.
- **Always carry `credentials` from `join_match`** on every `make_move`. Don't lose them.
- **Treat tool errors as information**, not failures. The error string tells you what went wrong (occupied cell, illegal phase, bad credentials). Read it, fix, try again.

## Narrating moves

After each `make_move`, narrate the move in plain language so the human sees what you did: "I'll play the center (cell 4)." Then say whose turn it is now.

When the human plays, narrate it back so you both stay in sync: "OK, you played the top-right corner. I'll move next."

## Surrendering or restarting

If the user wants to stop or start over, just do it. The server doesn't need a special tool for "give up" — abandoning the match is fine. Use `create_match` again to start fresh.

## Game-specific strategy

If a game has a sibling skill (e.g. `tictactoe-strategy`), let that skill drive your move choices. Without one, pick moves that:
- preserve your options
- block the opponent's threats
- aim for a clear winning line

When in doubt, narrate your reasoning so the user can correct you.

## Designed games (user-authored)

The server may expose user-designed games alongside built-ins. They appear in `list_games` with `userOwned: true`. If the active match's game has a `game://<owner>/<name>/guide` resource available via `resources/list`, **read it once before your first move**. The guide is the designer's rules-and-strategy notes; treat it the way you treat a `*-strategy` sibling skill for built-in games.

To design a new game with the user, invoke the `design-a-game` prompt.
