package match

import (
	"sync"
	"testing"

	"github.com/tjcran/boardgame-go/core"
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

func TestLifecycleMovedCarriesPrevStateAndLogDelta(t *testing.T) {
	// A move that ends a turn should produce PrevState.Ctx.Turn !=
	// State.Ctx.Turn (Turn advances because MaxMoves=1). LogDelta should
	// contain at least the move's own log entry.
	game := &core.Game{
		Name: "prev-state-test", MinPlayers: 2, MaxPlayers: 2,
		Setup: func(_ core.Ctx, _ any) core.G { return map[string]int{} },
		Moves: map[string]any{
			"act": core.MoveFn(func(mc *core.MoveContext, _ ...any) (core.G, error) {
				return mc.G, nil
			}),
		},
		Turn: &core.TurnConfig{MinMoves: 1, MaxMoves: 1}, // auto-end-turn after 1 move
	}
	store := storage.NewMemory()
	m := NewManager(store)
	m.MustRegister(game)

	var seen LifecycleEvent
	m.OnLifecycleKind(LifecycleMatchMoved, func(ev LifecycleEvent) {
		seen = ev
	})

	id, _ := m.Create("prev-state-test", CreateOptions{})
	alice, _ := m.Join(id, "alice", JoinOptions{})
	_, _ = m.Join(id, "bob", JoinOptions{})

	if _, err := m.Move(id, alice.PlayerID, alice.PlayerCredentials, "act", nil); err != nil {
		t.Fatalf("Move: %v", err)
	}

	if seen.Kind != LifecycleMatchMoved {
		t.Fatalf("did not observe LifecycleMatchMoved, got kind=%q", seen.Kind)
	}
	if seen.PrevState.Ctx.Turn == seen.State.Ctx.Turn {
		t.Errorf("expected Turn to advance: prev=%d post=%d",
			seen.PrevState.Ctx.Turn, seen.State.Ctx.Turn)
	}
	if len(seen.LogDelta) == 0 {
		t.Errorf("LogDelta should contain at least the move entry, got 0")
	}
	// Sanity: LogDelta length should match the growth of State.Log.
	wantDelta := len(seen.State.Log) - len(seen.PrevState.Log)
	if got := len(seen.LogDelta); got != wantDelta {
		t.Errorf("LogDelta length = %d, want %d", got, wantDelta)
	}
	// First entry in LogDelta should be the "act" move.
	if seen.LogDelta[0].Move != "act" {
		t.Errorf("LogDelta[0].Move = %q, want \"act\"", seen.LogDelta[0].Move)
	}
}

func TestLifecycleNonMoveEventsHaveZeroPrevState(t *testing.T) {
	// Non-move lifecycle events (created, joined, etc.) should leave
	// PrevState as the zero value and LogDelta nil.
	game := &core.Game{
		Name: "prev-state-nonmove", MinPlayers: 2, MaxPlayers: 2,
		Setup: func(_ core.Ctx, _ any) core.G { return nil },
		Moves: map[string]any{},
		Turn:  &core.TurnConfig{MinMoves: 1, MaxMoves: 1},
	}
	store := storage.NewMemory()
	m := NewManager(store)
	m.MustRegister(game)

	var seen LifecycleEvent
	m.OnLifecycleKind(LifecycleMatchJoined, func(ev LifecycleEvent) {
		seen = ev
	})

	id, _ := m.Create("prev-state-nonmove", CreateOptions{})
	_, _ = m.Join(id, "alice", JoinOptions{})

	if seen.Kind != LifecycleMatchJoined {
		t.Fatalf("did not observe Joined event")
	}
	if seen.PrevState.StateID != 0 || seen.PrevState.Ctx.Turn != 0 {
		t.Errorf("non-move event should have zero PrevState, got StateID=%d Turn=%d",
			seen.PrevState.StateID, seen.PrevState.Ctx.Turn)
	}
	if seen.LogDelta != nil {
		t.Errorf("non-move event should have nil LogDelta, got %v", seen.LogDelta)
	}
}

func TestLifecycleMoveRejectedFires(t *testing.T) {
	// Dispatch a move that core.ApplyContext rejects (here: an unknown
	// move name). Verify a LifecycleMatchMoveRejected event fires with
	// the expected fields.
	g := &core.Game{
		Name: "rejected-test", MinPlayers: 2, MaxPlayers: 2,
		Setup: func(_ core.Ctx, _ any) core.G { return map[string]int{} },
		Moves: map[string]any{
			"act": core.MoveFn(func(mc *core.MoveContext, _ ...any) (core.G, error) {
				return mc.G, nil
			}),
		},
		Turn: &core.TurnConfig{MinMoves: 1, MaxMoves: 1},
	}
	m := NewManager(storage.NewMemory())
	m.MustRegister(g)

	var seen LifecycleEvent
	m.OnLifecycleKind(LifecycleMatchMoveRejected, func(ev LifecycleEvent) {
		seen = ev
	})

	id, _ := m.Create("rejected-test", CreateOptions{})
	alice, _ := m.Join(id, "alice", JoinOptions{})
	_, _ = m.Join(id, "bob", JoinOptions{})

	// Dispatch a move that doesn't exist in the game's Moves map.
	_, err := m.Move(id, alice.PlayerID, alice.PlayerCredentials, "nope", nil)
	if err == nil {
		t.Fatal("expected error from unknown move, got nil")
	}

	if seen.Kind != LifecycleMatchMoveRejected {
		t.Fatalf("expected LifecycleMatchMoveRejected, got %q", seen.Kind)
	}
	if seen.MatchID != id {
		t.Errorf("MatchID = %q, want %q", seen.MatchID, id)
	}
	if seen.PlayerID != alice.PlayerID {
		t.Errorf("PlayerID = %q, want %q", seen.PlayerID, alice.PlayerID)
	}
	if seen.Move != "nope" {
		t.Errorf("Move = %q, want \"nope\"", seen.Move)
	}
	if seen.Err == nil {
		t.Errorf("Err should be non-nil for rejected event")
	}
	if seen.StateID < 0 {
		t.Errorf("StateID = %d, want >= 0", seen.StateID)
	}
}

func TestLifecycleMoveRejectedDoesNotFireForAcceptedMove(t *testing.T) {
	// A successful move fires LifecycleMatchMoved, not Rejected.
	g := &core.Game{
		Name: "no-spurious-reject", MinPlayers: 2, MaxPlayers: 2,
		Setup: func(_ core.Ctx, _ any) core.G { return map[string]int{} },
		Moves: map[string]any{
			"act": core.MoveFn(func(mc *core.MoveContext, _ ...any) (core.G, error) {
				return mc.G, nil
			}),
		},
		Turn: &core.TurnConfig{MinMoves: 1, MaxMoves: 1},
	}
	m := NewManager(storage.NewMemory())
	m.MustRegister(g)

	var rejectedCount int
	m.OnLifecycleKind(LifecycleMatchMoveRejected, func(ev LifecycleEvent) {
		rejectedCount++
	})

	id, _ := m.Create("no-spurious-reject", CreateOptions{})
	alice, _ := m.Join(id, "alice", JoinOptions{})
	_, _ = m.Join(id, "bob", JoinOptions{})

	if _, err := m.Move(id, alice.PlayerID, alice.PlayerCredentials, "act", nil); err != nil {
		t.Fatalf("Move: %v", err)
	}
	if rejectedCount != 0 {
		t.Errorf("Rejected event should not fire on accepted move, fired %d times", rejectedCount)
	}
}

func TestLifecycleMoveRejectedDoesNotFireForCredentialFailure(t *testing.T) {
	// Credential failures are filtered earlier in MoveReqCtx — before
	// core.ApplyContext runs. The Rejected event is for game-logic
	// rejections specifically; auth failures are a separate category.
	g := &core.Game{
		Name: "no-auth-reject", MinPlayers: 2, MaxPlayers: 2,
		Setup: func(_ core.Ctx, _ any) core.G { return map[string]int{} },
		Moves: map[string]any{
			"act": core.MoveFn(func(mc *core.MoveContext, _ ...any) (core.G, error) {
				return mc.G, nil
			}),
		},
		Turn: &core.TurnConfig{MinMoves: 1, MaxMoves: 1},
	}
	m := NewManager(storage.NewMemory())
	m.MustRegister(g)

	var rejectedCount int
	m.OnLifecycleKind(LifecycleMatchMoveRejected, func(ev LifecycleEvent) {
		rejectedCount++
	})

	id, _ := m.Create("no-auth-reject", CreateOptions{})
	alice, _ := m.Join(id, "alice", JoinOptions{})
	_, _ = m.Join(id, "bob", JoinOptions{})

	// Dispatch with wrong credentials. Should error, but NOT fire the
	// game-logic rejected event.
	_, err := m.Move(id, alice.PlayerID, "wrong-creds", "act", nil)
	if err == nil {
		t.Fatal("expected credential error")
	}
	if rejectedCount != 0 {
		t.Errorf("credential failure should not fire Rejected lifecycle event, fired %d times", rejectedCount)
	}
}
