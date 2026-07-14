package match

import (
	"testing"
	"time"

	"github.com/tjcran/boardgame-go/games/tictactoe"
	"github.com/tjcran/boardgame-go/plugins/random"
	"github.com/tjcran/boardgame-go/storage"
)

// TestCachedStoreDoesNotRollBackDecodedG reproduces the 2026-07-14
// incident: with a serializing inner store behind Cached, the first
// load rehydrates G via DecodeG — but the cached live State still
// carries the as-loaded rawG bytes. Every subsequent load re-ran the
// decode hooks against those stale bytes, overwriting the live G with
// the original snapshot while Ctx kept advancing: moves "applied" but
// the game rolled back every turn. FlatFile alone masks this (each Get
// re-reads and re-captures raw); the cache keeps the live struct.
func TestCachedStoreDoesNotRollBackDecodedG(t *testing.T) {
	dir := t.TempDir()
	flat, err := storage.NewFlatFile(dir)
	if err != nil {
		t.Fatalf("flatfile: %v", err)
	}
	wb, err := storage.NewWriteBehind(flat, storage.WriteBehindOpts{
		BatchSize: 32, FlushInterval: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("writebehind: %v", err)
	}
	cached := storage.NewCached(wb, storage.CacheOpts{})

	m := NewManager(cached)
	game := tictactoe.New()
	game.Plugins = append(game.Plugins, random.New(uint64(7)))
	m.MustRegister(game)

	id, err := m.Create("tic-tac-toe", CreateOptions{})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	alice, err := m.Join(id, "alice", JoinOptions{})
	if err != nil {
		t.Fatalf("join alice: %v", err)
	}
	bob, err := m.Join(id, "bob", JoinOptions{})
	if err != nil {
		t.Fatalf("join bob: %v", err)
	}

	// Force a serialization round-trip into the cache: drop the cached
	// entry so the next load decodes from disk (captures rawG).
	wb.Sync()
	cached.Invalidate(id)

	// Two moves through the cached live state. Pre-fix, the SECOND
	// move's load re-decoded the stale rawG and erased the first move.
	if _, err := m.Move(id, alice.PlayerID, alice.PlayerCredentials, "clickCell", []any{4}); err != nil {
		t.Fatalf("move 1: %v", err)
	}
	if _, err := m.Move(id, bob.PlayerID, bob.PlayerCredentials, "clickCell", []any{0}); err != nil {
		t.Fatalf("move 2: %v", err)
	}

	loaded, err := m.State(id)
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	s, ok := loaded.State.G.(*tictactoe.State)
	if !ok {
		t.Fatalf("G is %T, want *tictactoe.State", loaded.State.G)
	}
	if s.Cells[4] != "X" || s.Cells[0] != "O" {
		t.Fatalf("moves rolled back: cells=%+v (want X at 4, O at 0)", s.Cells)
	}
}
