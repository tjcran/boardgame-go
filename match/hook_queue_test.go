package match

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/storage"
)

var errStepFails = errors.New("hook queue test: pushed move failed")

// hookCascadeGame is a two-player game whose Turn.OnBegin pushes a move
// that always fails, on the turns failOn reports true for. attempts counts
// how often that move ran.
func hookCascadeGame(budget time.Duration, attempts *atomic.Int32, failOn func(turn int) bool) *core.Game {
	type state struct{ Pings int }
	return &core.Game{
		Name:       "hook-cascade",
		MinPlayers: 2,
		MaxPlayers: 2,
		Setup:      func(_ core.Ctx, _ any) core.G { return &state{} },
		Moves: map[string]any{
			"ping": core.MoveFn(func(mc *core.MoveContext, _ ...any) (core.G, error) {
				return &state{Pings: mc.G.(*state).Pings + 1}, nil
			}),
			"fails": core.MoveFn(func(mc *core.MoveContext, _ ...any) (core.G, error) {
				attempts.Add(1)
				return mc.G, errStepFails
			}),
		},
		Turn: &core.TurnConfig{
			TimeBudget: budget,
			OnBegin: func(mc *core.MoveContext) core.G {
				if failOn(mc.Ctx.Turn) {
					mc.Queue.Push(mc.Ctx.CurrentPlayer, "fails")
				}
				return mc.G
			},
		},
	}
}

func countMoved(m *Manager) (count func() int, unsub func()) {
	var mu sync.Mutex
	n := 0
	unsub = m.OnLifecycleKind(LifecycleMatchMoved, func(LifecycleEvent) {
		mu.Lock()
		n++
		mu.Unlock()
	})
	return func() int { mu.Lock(); defer mu.Unlock(); return n }, unsub
}

// TestAutoExpireFailedForcedTurnEndLeavesMatch: when the next turn's
// OnBegin pushes a failing move, AutoExpire returns the error and the
// match keeps its state; nothing is announced as a move.
func TestAutoExpireFailedForcedTurnEndLeavesMatch(t *testing.T) {
	var attempts atomic.Int32
	m := NewManager(storage.NewMemory())
	m.MustRegister(hookCascadeGame(time.Hour, &attempts, func(turn int) bool { return turn == 2 }))
	id, err := m.Create("hook-cascade", CreateOptions{})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	before, _ := m.State(id)
	moved, unsub := countMoved(m)
	defer unsub()

	if err := m.AutoExpire(id, before.State.Ctx.Turn); !errors.Is(err, errStepFails) {
		t.Fatalf("AutoExpire err = %v, want the pushed move's error", err)
	}
	after, _ := m.State(id)
	if after.State.Ctx.Turn != before.State.Ctx.Turn || after.State.StateID != before.State.StateID {
		t.Fatalf("state changed: turn %d->%d stateID %d->%d", before.State.Ctx.Turn, after.State.Ctx.Turn,
			before.State.StateID, after.State.StateID)
	}
	if n := moved(); n != 0 {
		t.Fatalf("LifecycleMatchMoved fired %d times for a failed expiry", n)
	}
	if n := attempts.Load(); n != 1 {
		t.Fatalf("failing move ran %d times, want 1", n)
	}
}

// TestAutoExpireFailureIsNotRetried: the timer is not re-armed after a
// failed forced turn end, so the failing cascade runs once rather than
// once per TimeBudget.
func TestAutoExpireFailureIsNotRetried(t *testing.T) {
	var attempts atomic.Int32
	m := NewManager(storage.NewMemory())
	m.MustRegister(hookCascadeGame(20*time.Millisecond, &attempts, func(turn int) bool { return turn == 2 }))
	id, err := m.Create("hook-cascade", CreateOptions{})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	deadline := time.Now().Add(time.Second)
	for attempts.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if attempts.Load() == 0 {
		t.Fatal("timer never fired")
	}
	// Ten budgets' worth: a re-armed timer would have retried several times.
	time.Sleep(200 * time.Millisecond)
	if n := attempts.Load(); n != 1 {
		t.Fatalf("failing forced turn end ran %d times, want 1 (timer re-armed)", n)
	}
	mm, _ := m.State(id)
	if mm.State.Ctx.Turn != 1 {
		t.Fatalf("turn = %d, want 1", mm.State.Ctx.Turn)
	}
}

// TestAutoExpireBumpsStateID: a successful forced turn end bumps the
// StateID once, so clients holding the previous one are told they are
// stale.
func TestAutoExpireBumpsStateID(t *testing.T) {
	m := NewManager(storage.NewMemory())
	g := timeoutGame()
	g.Turn.TimeBudget = time.Hour
	m.MustRegister(g)
	id, _ := m.Create("timeout", CreateOptions{})
	before, _ := m.State(id)
	if err := m.AutoExpire(id, before.State.Ctx.Turn); err != nil {
		t.Fatalf("AutoExpire: %v", err)
	}
	after, _ := m.State(id)
	if after.State.Ctx.Turn != before.State.Ctx.Turn+1 || after.State.StateID != before.State.StateID+1 {
		t.Fatalf("turn %d->%d stateID %d->%d, want both +1", before.State.Ctx.Turn, after.State.Ctx.Turn,
			before.State.StateID, after.State.StateID)
	}
}

// TestCreateRefusesFailedSetupCascade: a game whose setup hooks push a
// failing move would yield a match on which every move fails. Create
// reports the error and stores nothing.
func TestCreateRefusesFailedSetupCascade(t *testing.T) {
	var attempts atomic.Int32
	m := NewManager(storage.NewMemory())
	m.MustRegister(hookCascadeGame(0, &attempts, func(turn int) bool { return turn == 1 }))
	id, err := m.Create("hook-cascade", CreateOptions{})
	if !errors.Is(err, errStepFails) || id != "" {
		t.Fatalf("Create = (%q, %v), want no match and the pushed move's error", id, err)
	}
	if list, _ := m.store.List("hook-cascade"); len(list) != 0 {
		t.Fatalf("store holds %d matches, want none", len(list))
	}
}

// TestResetAndPlayAgainRefuseFailedSetupCascade: Reset keeps the current
// state and PlayAgain creates nothing when the new setup's cascade fails.
func TestResetAndPlayAgainRefuseFailedSetupCascade(t *testing.T) {
	var attempts atomic.Int32
	var failSetup atomic.Bool
	m := NewManager(storage.NewMemory())
	m.MustRegister(hookCascadeGame(0, &attempts, func(turn int) bool { return turn == 1 && failSetup.Load() }))
	id, err := m.Create("hook-cascade", CreateOptions{})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	alice, _ := m.Join(id, "alice", JoinOptions{})
	_, _ = m.Join(id, "bob", JoinOptions{})
	if _, err := m.Move(id, alice.PlayerID, alice.PlayerCredentials, "ping", nil); err != nil {
		t.Fatalf("ping: %v", err)
	}
	before, _ := m.State(id)

	failSetup.Store(true)
	if err := m.Reset(id, alice.PlayerID, alice.PlayerCredentials); !errors.Is(err, errStepFails) {
		t.Fatalf("Reset err = %v, want the pushed move's error", err)
	}
	after, _ := m.State(id)
	if after.State.StateID != before.State.StateID || after.State.G == nil {
		t.Fatalf("Reset replaced the state: stateID %d->%d", before.State.StateID, after.State.StateID)
	}
	if next, err := m.PlayAgain(id, alice.PlayerID, alice.PlayerCredentials, 0, nil, true); !errors.Is(err, errStepFails) || next != "" {
		t.Fatalf("PlayAgain = (%q, %v), want no match and the pushed move's error", next, err)
	}
}
