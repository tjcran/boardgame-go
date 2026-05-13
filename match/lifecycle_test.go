package match

import (
	"sync"
	"testing"

	"github.com/tjcran/boardgame-go/games/tictactoe"
	"github.com/tjcran/boardgame-go/storage"
)

func TestLifecycleHooksFireOnExpectedTransitions(t *testing.T) {
	m := NewManager(storage.NewMemory())
	m.MustRegister(tictactoe.New())

	var (
		mu     sync.Mutex
		events []LifecycleEventKind
	)
	unsub := m.OnLifecycle(func(ev LifecycleEvent) {
		mu.Lock()
		events = append(events, ev.Kind)
		mu.Unlock()
	})
	defer unsub()

	id, _ := m.Create("tic-tac-toe", CreateOptions{})
	alice, _ := m.Join(id, "alice", JoinOptions{})
	bob, _ := m.Join(id, "bob", JoinOptions{})

	// Play X across the top row.
	plays := []struct {
		pid, creds string
		cell       int
	}{
		{alice.PlayerID, alice.PlayerCredentials, 0},
		{bob.PlayerID, bob.PlayerCredentials, 3},
		{alice.PlayerID, alice.PlayerCredentials, 1},
		{bob.PlayerID, bob.PlayerCredentials, 4},
		{alice.PlayerID, alice.PlayerCredentials, 2},
	}
	for _, p := range plays {
		_, err := m.Move(id, p.pid, p.creds, "clickCell", []any{p.cell})
		if err != nil {
			t.Fatalf("move: %v", err)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	want := []LifecycleEventKind{
		LifecycleMatchCreated,
		LifecycleMatchJoined, LifecycleMatchJoined,
		LifecycleMatchMoved, LifecycleMatchMoved, LifecycleMatchMoved,
		LifecycleMatchMoved, LifecycleMatchMoved,
		LifecycleMatchGameOver,
	}
	if len(events) != len(want) {
		t.Fatalf("event count: got %d want %d (events=%v)", len(events), len(want), events)
	}
	for i, e := range want {
		if events[i] != e {
			t.Fatalf("event %d: got %s want %s", i, events[i], e)
		}
	}
}

func TestLifecycleKindFilter(t *testing.T) {
	m := NewManager(storage.NewMemory())
	m.MustRegister(tictactoe.New())

	var joins int
	unsub := m.OnLifecycleKind(LifecycleMatchJoined, func(ev LifecycleEvent) {
		joins++
	})
	defer unsub()

	id, _ := m.Create("tic-tac-toe", CreateOptions{})
	_, _ = m.Join(id, "alice", JoinOptions{})
	_, _ = m.Join(id, "bob", JoinOptions{})

	if joins != 2 {
		t.Fatalf("expected 2 joined events, got %d", joins)
	}
}
