package match

import (
	"errors"
	"sync"
	"testing"

	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/storage"
)

// counterGame never ends, so a test can apply as many moves as it likes.
func counterGame() *core.Game {
	type state struct{ N int }
	return &core.Game{
		Name:       "counter",
		MinPlayers: 1,
		MaxPlayers: 2,
		Setup:      func(_ core.Ctx, _ any) core.G { return &state{} },
		Moves: map[string]any{
			"tick": core.MoveFn(func(mc *core.MoveContext, _ ...any) (core.G, error) {
				return &state{N: mc.G.(*state).N + 1}, nil
			}),
		},
	}
}

// newCachedManager uses storage.Cached on purpose: it hands every load the
// same live *storage.Match, which is what makes an unlocked read unsafe.
func newCachedManager(t *testing.T) (*Manager, string, string, string) {
	t.Helper()
	m := NewManager(storage.NewCached(storage.NewMemory(), storage.CacheOpts{}))
	m.Register(counterGame())
	id, err := m.Create("counter", CreateOptions{NumPlayers: 1})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	pid, creds := joinHelper(t, m, id, "solo")
	return m, id, pid, creds
}

// Run with -race: Snapshot must not race the in-place State assignment a
// concurrent move makes on the cached match.
func TestSnapshotConcurrentWithMoves(t *testing.T) {
	m, id, pid, creds := newCachedManager(t)
	const moves = 200

	done := make(chan struct{})
	var wg sync.WaitGroup
	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			last := -1
			for {
				select {
				case <-done:
					return
				default:
				}
				snap, err := m.Snapshot(id)
				if err != nil {
					t.Errorf("snapshot: %v", err)
					return
				}
				if snap.State.StateID < last {
					t.Errorf("StateID went backwards: %d after %d", snap.State.StateID, last)
					return
				}
				last = snap.State.StateID
				for _, e := range snap.State.Log {
					_ = e.PlayerID
				}
				for _, p := range snap.Players {
					_ = p.Credentials
				}
				_ = snap.State.Ctx.Gameover
			}
		}()
	}

	for i := 0; i < moves; i++ {
		if _, err := m.Move(id, pid, creds, "tick", nil); err != nil {
			t.Fatalf("move %d: %v", i, err)
		}
	}
	close(done)
	wg.Wait()
}

func TestSnapshotIsUnaffectedByLaterMoves(t *testing.T) {
	m, id, pid, creds := newCachedManager(t)
	if _, err := m.Move(id, pid, creds, "tick", nil); err != nil {
		t.Fatalf("move: %v", err)
	}
	snap, err := m.Snapshot(id)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	stateID, logLen := snap.State.StateID, len(snap.State.Log)

	for i := 0; i < 3; i++ {
		if _, err := m.Move(id, pid, creds, "tick", nil); err != nil {
			t.Fatalf("move: %v", err)
		}
	}
	if snap.State.StateID != stateID || len(snap.State.Log) != logLen {
		t.Fatalf("snapshot changed: stateID %d->%d, log %d->%d",
			stateID, snap.State.StateID, logLen, len(snap.State.Log))
	}

	// Writes to the snapshot stay in the snapshot.
	snap.Players[0].Name = "changed"
	snap.State.Log = append(snap.State.Log, core.LogEntry{PlayerID: "x"})
	live, err := m.Snapshot(id)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if live.Players[0].Name != "solo" {
		t.Fatalf("player write leaked into the live match: %q", live.Players[0].Name)
	}
	if live.State.Log[logLen].PlayerID == "x" {
		t.Fatal("log append on the snapshot overwrote the live log")
	}
}

func TestSnapshotMissingMatch(t *testing.T) {
	m := NewManager(storage.NewMemory())
	if _, err := m.Snapshot("nope"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("want storage.ErrNotFound, got %v", err)
	}
}
