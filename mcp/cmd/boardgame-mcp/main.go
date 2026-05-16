// Command boardgame-mcp serves the boardgame-go match runtime as an MCP
// server, letting an external LLM (Claude.ai connector, Claude Desktop,
// Claude Code, Cursor, …) play games against a human.
//
// Two transports:
//
//	# stdio mode (default) — local play, single user
//	boardgame-mcp serve --transport=stdio [--db PATH]
//
//	# HTTP mode — hosted Claude app, multi-tenant when --jwks-url is set
//	boardgame-mcp serve --transport=http --port=8080 \
//	    --jwks-url=https://issuer/.well-known/jwks.json \
//	    --issuer=https://issuer/ --audience=boardgame-mcp [--db PATH]
//
// Wire into Claude Code (stdio mode):
//
//	claude mcp add boardgame /path/to/boardgame-mcp serve
//
// Then start any session and ask Claude to play a registered game.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/tjcran/boardgame-go/games/tictactoe"
	"github.com/tjcran/boardgame-go/match"
	mcppkg "github.com/tjcran/boardgame-go/mcp"
	"github.com/tjcran/boardgame-go/storage"
	pgstore "github.com/tjcran/boardgame-go/storage/postgres"
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
  boardgame-mcp serve [flags]   Start the MCP server.

FLAGS for serve:
  --transport stdio|http   Transport. Default stdio.
  --db PATH                Path to SQLite database. Default in-memory.

HTTP-mode flags:
  --port N                 Listen port. Default 8080.
  --jwks-url URL           OIDC JWKS endpoint. When set, requests must
                           carry a verified Bearer JWT and matches are
                           scoped by the verified subject (multi-tenant).
                           When empty, all requests share a single
                           "anonymous" tenant (dev / single-user mode).
  --issuer URL             Required iss claim. Default: any.
  --audience AUD           Required aud claim. Default: any.

EXAMPLES:
  # Local stdio:
  claude mcp add boardgame $(which boardgame-mcp) serve

  # Persistent local stdio:
  boardgame-mcp serve --db ~/.boardgame/matches.db

  # Hosted HTTP with auth:
  boardgame-mcp serve --transport=http --port=8080 \
      --jwks-url=https://issuer/.well-known/jwks.json \
      --issuer=https://issuer/ --audience=boardgame-mcp`)
}

type serveFlags struct {
	transport   string
	dbPath      string
	databaseURL string
	skillsDir   string
	port        int
	jwksURL     string
	issuer      string
	audience    string
}

// defaultSkillsDir is the on-disk location designed games are written to
// in stdio mode when no --database-url and no explicit --skills-dir is
// set. Each game lives at <root>/<name>/{SKILL.md,spec.star}. Returns ""
// if $HOME can't be resolved — caller falls back to in-memory.
func defaultSkillsDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".claude", "skills", "games")
}

func runServe(argv []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	cfg := serveFlags{}
	fs.StringVar(&cfg.transport, "transport", "stdio", "Transport: stdio | http")
	fs.StringVar(&cfg.dbPath, "db", "", "Path to SQLite database file (default: in-memory)")
	fs.StringVar(&cfg.databaseURL, "database-url", "", "Postgres DSN. When set, overrides --db and is used for both match state AND ownership. Falls back to $DATABASE_URL if empty.")
	fs.StringVar(&cfg.skillsDir, "skills-dir", defaultSkillsDir(), "Directory holding designed games as on-disk skills (stdio mode only; ignored when --database-url is set). Pass an empty string to keep designed games in memory only (not persistent).")
	fs.IntVar(&cfg.port, "port", 8080, "HTTP listen port (http transport only)")
	fs.StringVar(&cfg.jwksURL, "jwks-url", "", "OIDC JWKS URL for OAuth verification (http transport only)")
	fs.StringVar(&cfg.issuer, "issuer", "", "Required JWT issuer (http transport only)")
	fs.StringVar(&cfg.audience, "audience", "", "Required JWT audience (http transport only)")
	if err := fs.Parse(argv); err != nil {
		return err
	}
	// $DATABASE_URL is the Cloud Run / 12-factor convention — fall back
	// to it if --database-url wasn't explicitly passed.
	if cfg.databaseURL == "" {
		cfg.databaseURL = os.Getenv("DATABASE_URL")
	}

	// Logger goes to stderr — in stdio mode, stdout is reserved for JSON-RPC.
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	store, closeStore, err := openStorage(cfg, logger)
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}
	defer closeStore()

	mgr := match.NewManager(store)
	mgr.Logger = logger
	mgr.MustRegister(tictactoe.New())

	ownership, closeOwnership, err := openOwnership(cfg, logger)
	if err != nil {
		return fmt.Errorf("open ownership store: %w", err)
	}
	defer closeOwnership()

	// Open the user-games store. Selection (highest precedence first):
	//   1. --database-url set  → Postgres (hosted / multi-tenant deployments)
	//   2. --skills-dir non-empty → skills-on-disk (stdio default; human-
	//      readable, hand-editable files at $HOME/.claude/skills/games)
	//   3. otherwise → in-memory (ephemeral; usually only when --skills-dir
	//      was explicitly emptied)
	var ugStore mcppkg.UserGameStore
	switch {
	case cfg.databaseURL != "":
		pgUG, err := mcppkg.OpenPostgresUserGames(cfg.databaseURL)
		if err != nil {
			return fmt.Errorf("open user_games: %w", err)
		}
		defer pgUG.Close()
		ugStore = pgUG
	case cfg.skillsDir != "":
		sk, err := mcppkg.OpenSkillsDirUserGames(cfg.skillsDir)
		if err != nil {
			return fmt.Errorf("open skills dir: %w", err)
		}
		ugStore = sk
		logger.Info("user games store", "kind", "skills-dir", "path", cfg.skillsDir)
	default:
		ugStore = mcppkg.NewInMemoryUserGames()
		logger.Info("user games store", "kind", "in-memory", "note", "designed games will not persist across restarts")
	}
	registry := mcppkg.NewUserAwareRegistry(mgr, ugStore)
	if err := registry.ReplayFromStore(context.Background()); err != nil {
		return fmt.Errorf("replay user games: %w", err)
	}
	if all, err := ugStore.ListAll(context.Background()); err == nil {
		logger.Info("user games replayed", "count", len(all))
	}

	tools := &mcppkg.Tools{Manager: mgr, Ownership: ownership, Registry: registry}

	srv := mcppkg.NewServer(mcppkg.ServerInfo{
		Name:    "boardgame-mcp",
		Version: version,
	}, mcppkg.DefaultInstructions)
	mcppkg.RegisterTools(srv, tools)
	mcppkg.RegisterDefaultPrompts(srv)
	mcppkg.RegisterDesignAGamePrompt(srv)
	srv.WireGuideResources(registry)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	switch cfg.transport {
	case "stdio":
		logger.Info("boardgame-mcp serving stdio",
			"games", mgr.GameNames(), "db", dbDescriptor(cfg))
		return srv.ServeStdio(ctx, os.Stdin, os.Stdout, os.Stderr)
	case "http":
		return runHTTP(ctx, srv, mgr, cfg, logger)
	default:
		return fmt.Errorf("unknown transport %q (want stdio | http)", cfg.transport)
	}
}

func runHTTP(ctx context.Context, srv *mcppkg.Server, mgr *match.Manager, cfg serveFlags, logger *slog.Logger) error {
	handler := srv.HTTPHandler()
	authMode := "none"
	if cfg.jwksURL != "" {
		v := &mcppkg.JWTVerifier{
			JWKSURL:  cfg.jwksURL,
			Issuer:   cfg.issuer,
			Audience: cfg.audience,
		}
		handler = mcppkg.AuthMiddleware(v)(handler)
		authMode = "jwt"
	} else {
		// No-auth path: attach a fixed "anonymous" userID to every request so
		// downstream code has a non-empty userID if Ownership is ever turned
		// on later. Doesn't enable scoping by itself (Ownership stays nil).
		handler = anonymousMiddleware(handler)
	}

	mux := http.NewServeMux()
	mux.Handle("/mcp", handler)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})

	addr := fmt.Sprintf(":%d", cfg.port)
	s := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = s.Shutdown(shutdownCtx)
	}()

	logger.Info("boardgame-mcp serving http",
		"addr", addr, "auth", authMode,
		"games", mgr.GameNames(), "db", dbDescriptor(cfg))

	if err := s.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func anonymousMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(mcppkg.WithUserID(r.Context(), "anonymous")))
	})
}

// openStorage picks a Storage backend based on flags, in precedence order:
//
//	--database-url  (or $DATABASE_URL)  → Postgres
//	--db PATH                           → SQLite
//	neither                             → in-memory
func openStorage(cfg serveFlags, logger *slog.Logger) (storage.Storage, func(), error) {
	if cfg.databaseURL != "" {
		s, err := pgstore.Open(cfg.databaseURL)
		if err != nil {
			return nil, nil, fmt.Errorf("postgres match store: %w", err)
		}
		return s, func() {
			if err := s.Close(); err != nil {
				logger.Warn("close postgres match store", "err", err)
			}
		}, nil
	}
	if cfg.dbPath == "" {
		return storage.NewMemory(), func() {}, nil
	}
	if dir := filepath.Dir(cfg.dbPath); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, nil, fmt.Errorf("mkdir %s: %w", dir, err)
		}
	}
	s, err := sqlitestore.Open(cfg.dbPath)
	if err != nil {
		return nil, nil, err
	}
	return s, func() {
		if err := s.Close(); err != nil {
			logger.Warn("close sqlite", "err", err)
		}
	}, nil
}

// openOwnership picks an OwnershipStore based on flags.
//
//   - HTTP + Postgres: PostgresOwnership (durable, multi-instance safe)
//   - HTTP + JWT auth (no Postgres): MemoryOwnership (durable for the life
//     of the container — fine for single-instance dev deployments)
//   - everything else: nil (single-tenant mode — no scoping enforced)
func openOwnership(cfg serveFlags, logger *slog.Logger) (mcppkg.OwnershipStore, func(), error) {
	if cfg.transport != "http" {
		return nil, func() {}, nil
	}
	if cfg.databaseURL != "" {
		o, err := mcppkg.OpenPostgresOwnership(cfg.databaseURL)
		if err != nil {
			return nil, nil, fmt.Errorf("postgres ownership: %w", err)
		}
		return o, func() {
			if err := o.Close(); err != nil {
				logger.Warn("close postgres ownership", "err", err)
			}
		}, nil
	}
	if cfg.jwksURL != "" {
		// JWT auth on but no DB — scope by in-memory store. Loses state
		// on restart; only sound for single-instance deployments.
		return mcppkg.NewMemoryOwnership(), func() {}, nil
	}
	return nil, func() {}, nil
}

// dbDescriptor produces a short string for the startup log so operators
// can confirm at a glance which storage backend is active. The full
// Postgres DSN is intentionally elided — credentials shouldn't land in
// logs even when the operator passed them on the command line.
func dbDescriptor(cfg serveFlags) string {
	if cfg.databaseURL != "" {
		return "postgres"
	}
	if cfg.dbPath != "" {
		return cfg.dbPath
	}
	return "in-memory"
}
