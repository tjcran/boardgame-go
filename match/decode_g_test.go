package match

import (
	"testing"

	"github.com/tjcran/boardgame-go/games/tictactoe"
	"github.com/tjcran/boardgame-go/storage"
)

// TestFlatFileRoundTripPreservesTypedG reproduces issue #80: a code-backed
// game whose G is a concrete struct must survive a save/load cycle through
// any serializing store with its concrete type intact. Without the optional
// Game.DecodeG hook, loadMigrated leaves State.G as map[string]any and the
// first move that type-asserts panics.
func TestFlatFileRoundTripPreservesTypedG(t *testing.T) {
	dir := t.TempDir()
	flat, err := storage.NewFlatFile(dir)
	if err != nil {
		t.Fatalf("flatfile: %v", err)
	}
	m := NewManager(flat)
	m.MustRegister(tictactoe.New())

	id, err := m.Create("tic-tac-toe", CreateOptions{})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	alice, err := m.Join(id, "alice", JoinOptions{})
	if err != nil {
		t.Fatalf("join alice: %v", err)
	}
	if _, err := m.Join(id, "bob", JoinOptions{}); err != nil {
		t.Fatalf("join bob: %v", err)
	}

	loaded, err := m.State(id)
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	if _, ok := loaded.State.G.(*tictactoe.State); !ok {
		t.Fatalf("expected *tictactoe.State after flatfile reload, got %T", loaded.State.G)
	}

	// First move after persistence is where issue #80 panics: clickCell
	// does `mc.G.(*State)` and the load path has handed it map[string]any.
	if _, err := m.Move(id, alice.PlayerID, alice.PlayerCredentials, "clickCell", []any{4}); err != nil {
		t.Fatalf("move after reload: %v", err)
	}

	// Subsequent load must still re-type — the move just rewrote the file
	// from the typed value, so the next read decodes from typed JSON.
	loaded2, err := m.State(id)
	if err != nil {
		t.Fatalf("state after move: %v", err)
	}
	s, ok := loaded2.State.G.(*tictactoe.State)
	if !ok {
		t.Fatalf("expected *tictactoe.State after move + reload, got %T", loaded2.State.G)
	}
	if s.Cells[4] != "X" {
		t.Fatalf("expected X at cell 4, got %q (cells=%+v)", s.Cells[4], s.Cells)
	}
}
