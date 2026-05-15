# Local MCP mode

Play a game against Claude on your own machine — no servers, no auth, no internet.

For deploying as a remote Claude.ai connector with multi-tenancy and OAuth, see [hosted-app.md](hosted-app.md).

## Install

Pick whichever fits.

### A. Pre-built binary

Releases ship binaries for linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, and windows/amd64.

```sh
# Substitute your platform from the matrix above.
PLATFORM=linux-amd64
curl -L -o boardgame-mcp.tar.gz \
  "https://github.com/tjcran/boardgame-go/releases/latest/download/boardgame-mcp-${PLATFORM}.tar.gz"
tar xzf boardgame-mcp.tar.gz
chmod +x boardgame-mcp
sudo mv boardgame-mcp /usr/local/bin/   # or anywhere on $PATH
```

### B. Build from source

Requires a Go toolchain (1.23+).

```sh
git clone https://github.com/tjcran/boardgame-go
cd boardgame-go
go build -o boardgame-mcp ./mcp/cmd/boardgame-mcp
```

The MCP plugin is its own Go sub-module, so the root build (`go build ./...`) is unaffected.

### C. Docker

```sh
docker pull ghcr.io/tjcran/boardgame-mcp:latest
```

Multi-arch images cover `linux/amd64` and `linux/arm64`. Image is distroless/static, ~15 MB, runs as nonroot.

## Configure your client

### Claude Desktop

Open `claude_desktop_config.json`:

- **macOS:** `~/Library/Application Support/Claude/claude_desktop_config.json`
- **Windows:** `%APPDATA%\Claude\claude_desktop_config.json`
- **Linux:** `~/.config/Claude/claude_desktop_config.json`

Add one of these snippets to the `mcpServers` block. Restart Claude Desktop afterward.

**Using the native binary** (built or downloaded above):

```json
{
  "mcpServers": {
    "boardgame": {
      "command": "/absolute/path/to/boardgame-mcp",
      "args": ["serve"]
    }
  }
}
```

**With persistence** — matches survive restarts:

```json
{
  "mcpServers": {
    "boardgame": {
      "command": "/absolute/path/to/boardgame-mcp",
      "args": ["serve", "--db", "/Users/you/.boardgame/matches.db"]
    }
  }
}
```

**Using Docker** (no local binary needed):

```json
{
  "mcpServers": {
    "boardgame": {
      "command": "docker",
      "args": [
        "run", "--rm", "-i",
        "ghcr.io/tjcran/boardgame-mcp:latest",
        "serve"
      ]
    }
  }
}
```

For persistent matches via Docker, mount a volume and add `--db`:

```json
{
  "mcpServers": {
    "boardgame": {
      "command": "docker",
      "args": [
        "run", "--rm", "-i",
        "-v", "boardgame-data:/data",
        "ghcr.io/tjcran/boardgame-mcp:latest",
        "serve", "--db", "/data/matches.db"
      ]
    }
  }
}
```

### Claude Code

```sh
claude mcp add boardgame "$(which boardgame-mcp)" serve
# or, for persistence:
claude mcp add boardgame "$(which boardgame-mcp)" serve --db "$HOME/.boardgame/matches.db"
```

### Cursor (and other MCP-capable editors)

Most editors that support MCP use the same `mcpServers` JSON shape as Claude Desktop. Drop the snippet above into the editor's MCP config.

## Use

Start a session and say:

> Let's play tic-tac-toe. You go first.

Claude will call `create_match`, `join_match` to claim a seat, `list_legal_moves` before each of its moves, and `make_move` to play. Pass your own moves in plain language ("I'll take the bottom right corner") — Claude submits them on your behalf via the same tools.

Per-game strategy and the umbrella playing protocol ship as Claude Code skill files under [`mcp/skills/`](../skills/). Install with `claude plugin install` for sharper play.

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

Useful if your client can't see the server. Pipe JSON-RPC frames straight in:

```sh
printf '%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}' \
  '{"jsonrpc":"2.0","method":"notifications/initialized"}' \
  '{"jsonrpc":"2.0","id":2,"method":"tools/list"}' \
  '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"list_games","arguments":{}}}' \
| boardgame-mcp serve
```

You should see three JSON-RPC responses on stdout (one for `initialize`, one for `tools/list`, one for `tools/call`). Server logs go to stderr.

## Troubleshooting

**The client doesn't see the server.** Run the wire smoke test above. If it works on the command line but not in your MCP client, verify the path you registered is absolute and executable. Restart the client after editing its config.

**"game X does not implement Enumerate."** That game's `core.Game` doesn't set `Enumerate`. Either add it (12 lines for most games — see `games/tictactoe/tictactoe.go` for the canonical example) or play a game that does.

**Match state is gone after restart.** You're in in-memory mode. Add `--db PATH` to persist (creates parent directories automatically). With Docker, mount a volume so the SQLite file survives container churn.

**Docker errors about `--rm` or `-i`.** Both flags are required for stdio MCP servers — Claude Desktop talks to the container's stdin/stdout. `--rm` keeps disk clean (one container per session); `-i` keeps stdin open.
