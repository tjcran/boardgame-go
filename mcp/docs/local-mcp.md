# Local MCP mode: play boardgames against Claude in your terminal

This is the local-stdio mode of `boardgame-mcp`. It runs as a single binary on your machine and exposes registered boardgame-go games as MCP tools, so Claude Code (or any other MCP-capable client) can play a game against you with no servers, no auth, no internet.

A hosted Claude-app mode is on the way in a follow-up PR; for now this is the path for early adopters.

## Build

The MCP plugin is its own Go sub-module so root `go build ./...` users never pull its dependencies. Build the binary explicitly:

```sh
# from the repo root, with go.work tying the modules together
go build -o boardgame-mcp ./mcp/cmd/boardgame-mcp
```

For persistent matches across restarts, point it at a SQLite file (the directory is created on first run):

```sh
./boardgame-mcp serve --db ~/.boardgame/matches.db
```

Without `--db` it uses in-memory storage — fine for a one-off session, matches are lost when the binary exits.

## Wire into Claude Code

```sh
claude mcp add boardgame "$(pwd)/boardgame-mcp" serve
# or, for persistence:
claude mcp add boardgame "$(pwd)/boardgame-mcp" serve --db "$HOME/.boardgame/matches.db"
```

That registers `boardgame` as an MCP server. Start any Claude Code session and ask:

> Let's play tic-tac-toe. You go first.

Claude will call `create_match`, `join_match`, list legal moves, and play against you. The `boardgame-player` and `tictactoe-strategy` skills under `mcp/skills/` teach Claude the protocol and basic strategy — install them with `claude plugin install` for sharper play.

## What's exposed

Six MCP tools, all backed by `match.Manager`:

| Tool | What it does |
|------|--------------|
| `list_games` | Registered games + player-count bounds. |
| `create_match` | Start a fresh match (returns `matchID`). |
| `join_match` | Claim a seat (returns `playerID` + `credentials`). |
| `get_state` | Current state from a player's perspective (PlayerView-redacted), turn info, gameover. |
| `list_legal_moves` | Enumerate `(move, args)` pairs the given player may submit right now. |
| `make_move` | Submit one move. |

The server also publishes an `instructions` block during the MCP `initialize` handshake — a tiny protocol guide Claude reads automatically. The skill files above add per-game strategy on top.

## Wire-protocol smoke test

Useful if Claude Code can't see the server. Pipe JSON-RPC frames straight in:

```sh
printf '%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}' \
  '{"jsonrpc":"2.0","method":"notifications/initialized"}' \
  '{"jsonrpc":"2.0","id":2,"method":"tools/list"}' \
  '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"list_games","arguments":{}}}' \
| ./boardgame-mcp serve
```

You should see three JSON-RPC responses on stdout (one initialize result, one tools/list, one tools/call). Server logs go to stderr.

## Limitations (current PR)

- **Single-user.** No auth; whoever runs the binary owns every match. Multi-tenant + OAuth lands with the hosted mode.
- **Only tic-tac-toe** is registered out of the box. Love Letter (and any future game) needs `Game.Enumerate` to be MCP-playable — tic-tac-toe got it as part of this PR.
- **Stdio only.** Streamable HTTP / SSE transport is part of the hosted-mode PR.
- **No prompts surface yet.** MCP prompts (e.g. `/play-tictactoe`) live in the hosted PR where they replace per-game Claude Code skill files for connector users.

## Troubleshooting

**Claude Code can't see the server.** Run the wire smoke test above. If it works on the command line but not in Claude Code, verify the path you registered is absolute and executable.

**"game X does not implement Enumerate."** That game's `core.Game` doesn't set `Enumerate`. Either add it (12 lines for most games — see `games/tictactoe/tictactoe.go`) or play a game that does.

**Match state is gone after restart.** You're in in-memory mode. Add `--db PATH` to persist.
