package match

import (
	"encoding/json"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/storage"
)

// mutState is a pointer G its move changes in place and returns.
type mutState struct {
	N    int            `json:"n"`
	Seen map[string]int `json:"seen"`
}

// mutGame builds the in-place game. onDecode, when non-nil, runs at the
// start of every DecodeG.
func mutGame(onDecode func()) *core.Game {
	return &core.Game{
		Name:       "mut",
		MinPlayers: 1,
		MaxPlayers: 1,
		Setup:      func(_ core.Ctx, _ any) core.G { return &mutState{Seen: map[string]int{}} },
		DecodeG: func(raw json.RawMessage) (core.G, error) {
			if onDecode != nil {
				onDecode()
			}
			s := &mutState{}
			if err := json.Unmarshal(raw, s); err != nil {
				return nil, err
			}
			if s.Seen == nil {
				s.Seen = map[string]int{}
			}
			return s, nil
		},
		Moves: map[string]any{
			"bump": core.MoveFn(func(mc *core.MoveContext, _ ...any) (core.G, error) {
				s := mc.G.(*mutState)
				s.N++
				s.Seen[strconv.Itoa(s.N%8)] = s.N
				return s, nil
			}),
		},
	}
}

// Run with -race. Work done inside View (here, encoding the in-place G
// and the players) is serialised with moves, over a cached store whose
// loads all return the one live match.
func TestViewConcurrentWithInPlaceMoves(t *testing.T) {
	m := NewManager(storage.NewCached(storage.NewMemory(), storage.CacheOpts{}))
	m.MustRegister(mutGame(nil))
	id, err := m.Create("mut", CreateOptions{NumPlayers: 1})
	if err != nil {
		t.Fatal(err)
	}
	pid, creds := joinHelper(t, m, id, "solo")

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
				var n int
				err := m.View(id, func(mm *storage.Match) error {
					b, err := json.Marshal(struct {
						State   core.State
						Players []storage.Player
					}{mm.State, mm.Players})
					if err != nil {
						return err
					}
					var out struct{ State struct{ G mutState } }
					if err := json.Unmarshal(b, &out); err != nil {
						return err
					}
					n = out.State.G.N
					return nil
				})
				if err != nil {
					t.Errorf("view: %v", err)
					return
				}
				if n < last {
					t.Errorf("N went backwards: %d after %d", n, last)
					return
				}
				last = n
			}
		}()
	}
	for i := 0; i < moves; i++ {
		if _, err := m.Move(id, pid, creds, "bump", nil); err != nil {
			t.Fatalf("move %d: %v", i, err)
		}
	}
	close(done)
	wg.Wait()
}

func TestViewReturnsFnAndLoadErrors(t *testing.T) {
	m, id, _, _ := newCachedManager(t)
	sentinel := errors.New("from fn")
	if err := m.View(id, func(*storage.Match) error { return sentinel }); !errors.Is(err, sentinel) {
		t.Fatalf("View returned %v, want fn's error", err)
	}
	called := false
	err := m.View("no-such-match", func(*storage.Match) error { called = true; return nil })
	if !errors.Is(err, storage.ErrNotFound) || called {
		t.Fatalf("View on a missing match: err=%v called=%v", err, called)
	}
}

// A match read back from serialized storage carries raw G bytes until its
// first load decodes them. State, like every load, has to decode under
// the match lock: otherwise a move can decode, apply and store its result
// while State is still decoding, and State then writes its decoding of
// the old bytes over the live match, losing the move.
func TestStateLoadDoesNotRollBackConcurrentMove(t *testing.T) {
	flat, err := storage.NewFlatFile(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cached := storage.NewCached(flat, storage.CacheOpts{})

	var armed atomic.Bool
	entered, release := make(chan struct{}), make(chan struct{})
	m := NewManager(cached)
	m.MustRegister(mutGame(func() {
		if armed.CompareAndSwap(true, false) {
			close(entered)
			<-release
		}
	}))
	id, err := m.Create("mut", CreateOptions{NumPlayers: 1})
	if err != nil {
		t.Fatal(err)
	}
	pid, creds := joinHelper(t, m, id, "solo")
	if _, err := m.Move(id, pid, creds, "bump", nil); err != nil {
		t.Fatal(err)
	}
	cached.Invalidate(id) // next load decodes the stored bytes

	armed.Store(true)
	loaded := make(chan error, 1)
	go func() {
		_, err := m.State(id)
		loaded <- err
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("State never reached DecodeG")
	}

	moved := make(chan error, 1)
	go func() {
		_, err := m.Move(id, pid, creds, "bump", nil)
		moved <- err
	}()
	select {
	case err := <-moved:
		moved <- err // finished while State was mid-decode: unlocked
	case <-time.After(300 * time.Millisecond):
		// Waiting on the lock State holds: the fixed path.
	}
	close(release)
	if err := <-loaded; err != nil {
		t.Fatalf("State: %v", err)
	}
	if err := <-moved; err != nil {
		t.Fatalf("move: %v", err)
	}

	snap, err := m.Snapshot(id)
	if err != nil {
		t.Fatal(err)
	}
	if n := snap.State.G.(*mutState).N; n != 2 {
		t.Fatalf("G rolled back: N = %d after two moves (StateID %d)", n, snap.State.StateID)
	}
}
