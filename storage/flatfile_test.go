package storage

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tjcran/boardgame-go/core"
)

func makeMatch(id, game string) *Match {
	return &Match{
		ID:       id,
		GameName: game,
		State: core.State{
			G:   map[string]any{"score": 1},
			Ctx: core.Ctx{NumPlayers: 2, CurrentPlayer: "0", PlayOrder: []string{"0", "1"}, Turn: 1},
		},
		Players:   []Player{{ID: "0", Seat: "0", Name: "alice"}},
		CreatedAt: 1234567890,
	}
}

func TestFlatFileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	f, err := NewFlatFile(dir)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	m := makeMatch("abc", "ttt")
	if err := f.Create(m); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := f.Get("abc")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID != "abc" || got.GameName != "ttt" || got.Players[0].Name != "alice" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}

func TestFlatFileUpdateRequiresExistingMatch(t *testing.T) {
	f, _ := NewFlatFile(t.TempDir())
	if err := f.Update(makeMatch("missing", "ttt")); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestFlatFileListFiltersByGame(t *testing.T) {
	f, _ := NewFlatFile(t.TempDir())
	_ = f.Create(makeMatch("a1", "ttt"))
	_ = f.Create(makeMatch("a2", "ttt"))
	_ = f.Create(makeMatch("b1", "chess"))
	got, err := f.List("ttt")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 ttt matches, got %d", len(got))
	}
}

func TestFlatFileWipeRemovesFile(t *testing.T) {
	dir := t.TempDir()
	f, _ := NewFlatFile(dir)
	_ = f.Create(makeMatch("x", "ttt"))
	if err := f.Wipe("x"); err != nil {
		t.Fatalf("wipe: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "x.json")); !os.IsNotExist(err) {
		t.Fatalf("expected file gone, stat err=%v", err)
	}
}

func TestFlatFileGetReturnsNotFound(t *testing.T) {
	f, _ := NewFlatFile(t.TempDir())
	_, err := f.Get("ghost")
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
