// Command boardgame-mcp serves the boardgame-go match runtime as an MCP
// server, letting an external LLM (Claude.ai connector, Claude Desktop,
// Claude Code, Cursor, …) play games against a human.
//
// Local stdio mode (this PR):
//
//	boardgame-mcp serve [--db ~/.boardgame/matches.db]
//
// Hosted HTTP/SSE + OAuth mode lands in a follow-up PR.
//
// Wire the binary into Claude Code:
//
//	claude mcp add boardgame /path/to/boardgame-mcp serve
//
// Then start any session and ask Claude to play one of the registered games.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/tjcran/boardgame-go/games/tictactoe"
	"github.com/tjcran/boardgame-go/match"
	mcppkg "github.com/tjcran/boardgame-go/mcp"
	"github.com/tjcran/boardgame-go/storage"
	sqlitestore "github.com/tjcran/boardgame-go/storage/sqlite"
)

// version is the user-visible binary version, surfaced to MCP clients via
// the initialize handshake. Bumped on each release.
const version = "0.1.0-dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "boardgame-mcp:", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) < 2 {
		usage()
		return nil
	}
	switch os.Args[1] {
	case "serve":
		return runServe(os.Args[2:])
	case "-h", "--help", "help":
		usage()
		return nil
	default:
		usage()
		return fmt.Errorf("unknown command: %s", os.Args[1])
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `boardgame-mcp — Model Context Protocol server for boardgame-go

USAGE:
  boardgame-mcp serve [flags]   Start the MCP server (stdio transport).

FLAGS for serve:
  --db PATH      Path to the SQLite database file. Default: in-memory
                 (matches lost on exit — fine for one-off sessions).

EXAMPLES:
  # Wire into Claude Code:
  claude mcp add boardgame $(which boardgame-mcp) serve

  # Persistent matches across restarts:
  claude mcp add boardgame $(which boardgame-mcp) serve --db ~/.boardgame/matches.db`)
}

func runServe(argv []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	dbPath := fs.String("db", "", "Path to SQLite database file (default: in-memory)")
	if err := fs.Parse(argv); err != nil {
		return err
	}

	// Logger goes to stderr — stdout is reserved for JSON-RPC traffic.
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	store, closeStore, err := openStorage(*dbPath, logger)
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}
	defer closeStore()

	mgr := match.NewManager(store)
	mgr.Logger = logger
	mgr.MustRegister(tictactoe.New())

	tools := &mcppkg.Tools{Manager: mgr}
	srv := mcppkg.NewServer(mcppkg.ServerInfo{
		Name:    "boardgame-mcp",
		Version: version,
	}, mcppkg.DefaultInstructions)
	mcppkg.RegisterTools(srv, tools)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	logger.Info("boardgame-mcp serving stdio", "games", mgr.GameNames(), "db", dbDescriptor(*dbPath))
	return srv.ServeStdio(ctx, os.Stdin, os.Stdout, os.Stderr)
}

// openStorage returns a Storage backed by SQLite when dbPath is non-empty,
// otherwise an in-memory store. The closer is a no-op for memory storage.
func openStorage(dbPath string, logger *slog.Logger) (storage.Storage, func(), error) {
	if dbPath == "" {
		return storage.NewMemory(), func() {}, nil
	}
	// Ensure the parent directory exists — MCP users won't necessarily
	// have created ~/.boardgame themselves.
	if dir := filepath.Dir(dbPath); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, nil, fmt.Errorf("mkdir %s: %w", dir, err)
		}
	}
	s, err := sqlitestore.Open(dbPath)
	if err != nil {
		return nil, nil, err
	}
	return s, func() {
		if err := s.Close(); err != nil {
			logger.Warn("close sqlite", "err", err)
		}
	}, nil
}

func dbDescriptor(p string) string {
	if p == "" {
		return "in-memory"
	}
	return p
}
