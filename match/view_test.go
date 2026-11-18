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

// encSub records what the manager pushes to an EncodedSubscriber.
type encSub struct {
	pid     string
	encoded [][]byte
	patch   []bool
}

func (s *encSub) Send(core.State)                { panic("Send called on an EncodedSubscriber") }
func (s *encSub) SendChat(ChatMessage)           {}
func (s *encSub) SendMatchData([]storage.Player) {}
func (s *encSub) PlayerID() string               { return s.pid }
func (s *encSub) SendEncoded(_ core.State, b []byte, patch bool) {
	s.encoded = append(s.encoded, b)
	s.patch = append(s.patch, patch)
}

// plainSub is a Subscriber with no encoding support.
type plainSub struct{ sent []core.State }

func (s *plainSub) Send(st core.State)             { s.sent = append(s.sent, st) }
func (s *plainSub) SendChat(ChatMessage)           {}
func (s *plainSub) SendMatchData([]storage.Player) {}
func (s *plainSub) PlayerID() string               { return "" }

// Broadcast encodes each seat's view once per move and hands the same
// bytes to every subscriber of that seat (spectators being one "seat").
func TestBroadcastEncodesEachSeatOnce(t *testing.T) {
	for _, delta := range []bool{false, true} {
		g := &core.Game{
			Name: "enc", MinPlayers: 2, MaxPlayers: 2, DeltaState: delta,
			Setup: func(_ core.Ctx, _ any) core.G { return map[string]int{"n": 0} },
			PlayerView: func(gg core.G, _ core.Ctx, pid string) core.G {
				return map[string]any{"n": gg.(map[string]int)["n"], "viewer": pid}
			},
			Moves: map[string]any{
				"act": core.MoveFn(func(mc *core.MoveContext, _ ...any) (core.G, error) {
					return map[string]int{"n": mc.G.(map[string]int)["n"] + 1}, nil
				}),
			},
		}
		m := NewManager(storage.NewMemory())
		m.MustRegister(g)
		id, _ := m.Create("enc", CreateOptions{})
		alice, _ := m.Join(id, "alice", JoinOptions{})
		_, _ = m.Join(id, "bob", JoinOptions{})

		a1, a2 := &encSub{pid: alice.PlayerID}, &encSub{pid: alice.PlayerID}
		s1, s2 := &encSub{}, &encSub{}
		b1 := &encSub{pid: "1"}
		plain := &plainSub{}
		for _, s := range []Subscriber{a1, a2, s1, s2, b1, plain} {
			defer m.Subscribe(id, s)()
		}
		if _, err := m.Move(id, alice.PlayerID, alice.PlayerCredentials, "act", nil); err != nil {
			t.Fatal(err)
		}

		for _, s := range []*encSub{a1, a2, s1, s2, b1} {
			if len(s.encoded) != 1 || s.patch[0] != delta {
				t.Fatalf("delta=%v: subscriber %q got %d pushes, patch=%v", delta, s.pid, len(s.encoded), s.patch)
			}
		}
		same := func(x, y []byte) bool { return &x[0] == &y[0] }
		if !same(a1.encoded[0], a2.encoded[0]) || !same(s1.encoded[0], s2.encoded[0]) {
			t.Errorf("delta=%v: subscribers of one seat got separate encodings", delta)
		}
		if same(a1.encoded[0], s1.encoded[0]) || same(a1.encoded[0], b1.encoded[0]) {
			t.Errorf("delta=%v: different seats share an encoding", delta)
		}
		var view struct {
			G struct {
				N      int    `json:"n"`
				Viewer string `json:"viewer"`
			}
		}
		if err := json.Unmarshal(b1.encoded[0], &view); err != nil || view.G.N != 1 || view.G.Viewer != "1" {
			t.Errorf("delta=%v: seat 1 encoding = %s (err %v)", delta, b1.encoded[0], err)
		}
		if len(plain.sent) != 1 {
			t.Errorf("delta=%v: plain subscriber got %d pushes, want 1", delta, len(plain.sent))
		}
	}
}
