package sqlite_test

import (
	"path/filepath"
	"testing"

	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/storage"
	"github.com/tjcran/boardgame-go/storage/sqlite"
)

func openTemp(t *testing.T) *sqlite.Storage {
	t.Helper()
	dsn := "file:" + filepath.Join(t.TempDir(), "test.db") + "?_pragma=journal_mode(WAL)"
	s, err := sqlite.Open(dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func makeMatch(id, game string) *storage.Match {
	return &storage.Match{
		ID:       id,
		GameName: game,
		State: core.State{
			G:   map[string]any{"score": 7},
			Ctx: core.Ctx{NumPlayers: 2, CurrentPlayer: "0", PlayOrder: []string{"0", "1"}, Turn: 1},
		},
		Players:   []storage.Player{{ID: "0", Seat: "0", Name: "alice"}},
		CreatedAt: 1234,
	}
}

func TestSQLiteRoundTrip(t *testing.T) {
	s := openTemp(t)
	m := makeMatch("abc", "ttt")
	if err := s.Create(m); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := s.Get("abc")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID != "abc" || got.Players[0].Name != "alice" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}

func TestSQLiteUpdateRequiresExisting(t *testing.T) {
	s := openTemp(t)
	if err := s.Update(makeMatch("missing", "ttt")); err != storage.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestSQLiteListFiltersByGame(t *testing.T) {
	s := openTemp(t)
	_ = s.Create(makeMatch("a1", "ttt"))
	_ = s.Create(makeMatch("a2", "ttt"))
	_ = s.Create(makeMatch("b1", "chess"))
	got, err := s.List("ttt")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 ttt matches, got %d", len(got))
	}
}

func TestSQLiteWipeRemovesRow(t *testing.T) {
	s := openTemp(t)
	_ = s.Create(makeMatch("x", "ttt"))
	if err := s.Wipe("x"); err != nil {
		t.Fatalf("wipe: %v", err)
	}
	if _, err := s.Get("x"); err != storage.ErrNotFound {
		t.Fatalf("expected ErrNotFound after wipe, got %v", err)
	}
}

func TestSQLitePersistsAcrossOpens(t *testing.T) {
	dir := t.TempDir()
	dsn := "file:" + filepath.Join(dir, "persist.db")

	s1, err := sqlite.Open(dsn)
	if err != nil {
		t.Fatalf("open1: %v", err)
	}
	if err := s1.Create(makeMatch("x", "ttt")); err != nil {
		t.Fatalf("create: %v", err)
	}
	s1.Close()

	s2, err := sqlite.Open(dsn)
	if err != nil {
		t.Fatalf("open2: %v", err)
	}
	defer s2.Close()
	got, err := s2.Get("x")
	if err != nil {
		t.Fatalf("get after reopen: %v", err)
	}
	if got.ID != "x" {
		t.Fatalf("expected persisted match, got %+v", got)
	}
}
