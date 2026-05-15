# boardgame-mcp

An MCP (Model Context Protocol) server that exposes [boardgame-go](https://github.com/tjcran/boardgame-go) matches as tools so an LLM client — **Claude Desktop**, **Claude Code**, **Cursor**, or any other MCP-capable client — can play a game against a human.

This is an **optional plugin** living in its own Go sub-module (`mcp/go.mod`). The root engine's dependency graph stays unchanged for library consumers who don't want MCP. `go build ./...` from the repo root never pulls any of the code below.

## Quick start (local play)

Pick one of three install paths, then configure your client.

### A. Pre-built binary (no Go toolchain)

```sh
# linux/amd64 — substitute your platform: darwin-arm64, linux-arm64, windows-amd64, …
curl -L -o boardgame-mcp.tar.gz \
  https://github.com/tjcran/boardgame-go/releases/latest/download/boardgame-mcp-linux-amd64.tar.gz
tar xzf boardgame-mcp.tar.gz
chmod +x boardgame-mcp
```

### B. Build from source

```sh
git clone https://github.com/tjcran/boardgame-go
cd boardgame-go
go build -o boardgame-mcp ./mcp/cmd/boardgame-mcp
```

### C. Docker

```sh
docker pull ghcr.io/tjcran/boardgame-mcp:latest
```

Then add the server to your client — full snippets in [docs/local-mcp.md](docs/local-mcp.md). The shortest path:

```sh
# Claude Code:
claude mcp add boardgame "$(pwd)/boardgame-mcp" serve
```

Start any session and say *"let's play tic-tac-toe"*.

## Hosted mode (Claude.ai connector)

For deploying boardgame-mcp as a remote MCP connector — HTTP transport, OAuth 2.1, Postgres-backed multi-tenancy, Cloud Run — see [docs/hosted-app.md](docs/hosted-app.md).

## Tool surface

| Tool | Purpose |
|------|---------|
| `list_games` | Registered games + player-count bounds |
| `create_match` | Start a fresh match |
| `join_match` | Claim a seat (issues credentials) |
| `get_state` | Current state, PlayerView-redacted for one player |
| `list_legal_moves` | Enumerate legal `(move, args)` for the current player |
| `make_move` | Submit a move |

All backed by `match.Manager`. Schemas live in [`mcp/register.go`](register.go).

## Layout

| Path | Purpose |
|------|---------|
| `mcp/cmd/boardgame-mcp/` | The binary |
| `mcp/server.go` | JSON-RPC 2.0 dispatch (transport-agnostic) |
| `mcp/transport_http.go` | HTTP transport |
| `mcp/tools.go` | Six tool handlers (plain Go funcs) |
| `mcp/register.go` | Tool/prompt MCP-schema registration |
| `mcp/prompts.go` | MCP prompts (`play-tictactoe`) |
| `mcp/auth.go` | OAuth 2.1 JWT verifier + middleware |
| `mcp/ownership.go` | Per-user match ownership (memory) |
| `mcp/ownership_pg.go` | Per-user match ownership (Postgres) |
| `mcp/skills/` | Claude Code skill files for local mode |
| `mcp/deploy/` | Dockerfile, Cloud Run spec |
| `mcp/docs/` | Operator + user guides |
