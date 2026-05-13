package postgres_test

import (
	"os"
	"strings"
	"testing"

	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/storage"
	"github.com/tjcran/boardgame-go/storage/postgres"
)

// dsn returns the test DSN or "" to skip. Postgres tests need a real
// server because there's no in-memory analogue (unlike Redis +
// miniredis). Spin one up locally with:
//
//	docker run --rm -p 5432:5432 -e POSTGRES_PASSWORD=test postgres:16
//
// and run:
//
//	BOARDGAMEGO_POSTGRES_DSN='postgres://postgres:test@localhost:5432/postgres?sslmode=disable' \
//	  go test ./storage/postgres/...
func dsn(t *testing.T) string {
	d := os.Getenv("BOARDGAMEGO_POSTGRES_DSN")
	if d == "" {
		t.Skip("set BOARDGAMEGO_POSTGRES_DSN to run Postgres integration tests")
	}
	return d
}

func openTest(t *testing.T) *postgres.Storage {
	t.Helper()
	s, err := postgres.Open(dsn(t))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// Best-effort cleanup of leftover rows from prior failed runs.
	t.Cleanup(func() {
		_ = s.Close()
	})
	return s
}

// wipeAll removes every match the test created. We use a prefix per test
// because multiple postgres tests can race against the same DB.
func wipeAll(t *testing.T, s *postgres.Storage, prefix string) {
	t.Helper()
	matches, err := s.List("")
	if err != nil {
		return
	}
	for _, m := range matches {
		if strings.HasPrefix(m.ID, prefix) {
			_ = s.Wipe(m.ID)
		}
	}
}

func makeMatch(id, game string) *storage.Match {
	return &storage.Match{
		ID:       id,
		GameName: game,
		State: core.State{
			G:   map[string]any{"score": 1},
			Ctx: core.Ctx{NumPlayers: 2, CurrentPlayer: "0", PlayOrder: []string{"0", "1"}, Turn: 1},
		},
		Players:   []storage.Player{{ID: "0", Seat: "0", Name: "alice"}},
		CreatedAt: 1234,
	}
}

func TestPostgresRoundTrip(t *testing.T) {
	s := openTest(t)
	defer wipeAll(t, s, "rt-")

	m := makeMatch("rt-abc", "ttt")
	if err := s.Create(m); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := s.Get("rt-abc")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Players[0].Name != "alice" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}

func TestPostgresUpdateRequiresExisting(t *testing.T) {
	s := openTest(t)
	if err := s.Update(makeMatch("up-missing", "ttt")); err != storage.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestPostgresListFiltersByGame(t *testing.T) {
	s := openTest(t)
	defer wipeAll(t, s, "lst-")

	_ = s.Create(makeMatch("lst-a1", "ttt"))
	_ = s.Create(makeMatch("lst-a2", "ttt"))
	_ = s.Create(makeMatch("lst-b1", "chess"))
	got, err := s.List("ttt")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	count := 0
	for _, m := range got {
		if strings.HasPrefix(m.ID, "lst-") {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("expected 2 ttt matches under lst-, got %d", count)
	}
}

func TestPostgresWipe(t *testing.T) {
	s := openTest(t)
	defer wipeAll(t, s, "wp-")

	_ = s.Create(makeMatch("wp-x", "ttt"))
	if err := s.Wipe("wp-x"); err != nil {
		t.Fatalf("wipe: %v", err)
	}
	if _, err := s.Get("wp-x"); err != storage.ErrNotFound {
		t.Fatalf("expected ErrNotFound after wipe, got %v", err)
	}
}
