// Package mcp exposes boardgame-go matches as MCP (Model Context Protocol)
// tools so an external LLM client — Claude.ai, Claude Desktop, Claude Code,
// Cursor — can play a registered game against a human.
//
// This package is an optional plugin: it lives in its own Go module so the
// boardgame-go root module's dependency graph stays unchanged for users who
// don't want MCP. Build the binary explicitly with
//
//	go build ./mcp/cmd/boardgame-mcp
//
// from the repo root (with go.work tying both modules together).
//
// # Architecture
//
// Tool handlers in tools.go are plain Go functions that wrap match.Manager
// methods. They take a userID for multi-tenant scoping and return
// JSON-safe values. They have no dependency on any MCP SDK, which keeps
// tests fast and lets us swap the transport layer without touching game
// logic.
//
// server.go binds those handlers to an MCP SDK and exposes them over a
// transport (stdio for local play, Streamable HTTP/SSE + OAuth for the
// hosted Claude app — added in a follow-up PR).
//
// # Tool surface
//
//	list_games        — registered game definitions
//	create_match      — start a fresh match
//	join_match        — claim a seat
//	get_state         — fetch player-redacted state + turn info + Gameover
//	list_legal_moves  — enumerate legal (move, args) for the current seat
//	make_move         — submit a move
//
// list_legal_moves is what makes LLM play viable: the model never has to
// guess a move schema — it receives the structured action list and picks
// one. See games/tictactoe for the canonical game contract.
package mcp
