package match

import (
	"context"
	"errors"
	"testing"

	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/storage"
)

// noopOCCGame: like pingPongGame but multi-move (no auto-end) so we
// can hand-craft StateID progressions for the OCC test.
func noopOCCGame() *core.Game {
	return &core.Game{
		Name:       "occ",
		MinPlayers: 1,
		MaxPlayers: 1,
		Setup:      func(_ core.Ctx, _ any) core.G { return struct{}{} },
		Moves: map[string]any{
			"noop": core.MoveFn(func(mc *core.MoveContext, _ ...any) (core.G, error) {
				return mc.G, nil
			}),
		},
	}
}

func TestUpdateIfStateIDMemoryRejectsStaleWrites(t *testing.T) {
	mem := storage.NewMemory()
	// Seed a match at StateID=5.
	m := &storage.Match{
		ID: "x", GameName: "occ",
		State:   core.State{StateID: 5, Ctx: core.Ctx{NumPlayers: 1, CurrentPlayer: "0", PlayOrder: []string{"0"}, Turn: 1}},
		Players: []storage.Player{{ID: "0", Seat: "0", Credentials: "c"}},
	}
	if err := mem.Create(m); err != nil {
		t.Fatalf("create: %v", err)
	}

	// A caller who saw StateID=5 lands a Write — should succeed.
	m2 := *m
	m2.State.StateID = 6
	if err := mem.UpdateIfStateID(&m2, 5); err != nil {
		t.Fatalf("first OCC update: %v", err)
	}
	// A stale caller (still claiming StateID=5) should hit ErrConflict.
	if err := mem.UpdateIfStateID(&m2, 5); !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("expected ErrConflict on stale OCC, got %v", err)
	}
}

// concurrentConflictingStore wraps Memory and injects exactly one stale
// state into the next loadMigrated call after a write — simulating a
// peer Manager racing on the same row.
type concurrentConflictingStore struct {
	*storage.Memory
	stalesLeft int
}

func (c *concurrentConflictingStore) UpdateIfStateID(m *storage.Match, expected int) error {
	if c.stalesLeft > 0 {
		c.stalesLeft--
		return storage.ErrConflict
	}
	return c.Memory.UpdateIfStateID(m, expected)
}

func TestManagerOCCRetriesAndSucceeds(t *testing.T) {
	store := &concurrentConflictingStore{
		Memory:     storage.NewMemory(),
		stalesLeft: 2, // first two writes lose the CAS race
	}
	m := NewManager(store)
	m.UseOptimisticConcurrency = true
	m.MustRegister(noopOCCGame())

	id, err := m.Create("occ", CreateOptions{})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	alice, _ := m.Join(id, "alice", JoinOptions{})

	// MoveReqCtx should retry past the injected conflicts.
	state, err := m.MoveReqCtx(context.Background(), id, alice.PlayerID, alice.PlayerCredentials,
		core.MoveRequest{Move: "noop"})
	if err != nil {
		t.Fatalf("move after retries: %v", err)
	}
	if state.StateID == 0 {
		t.Fatalf("expected StateID > 0 after applied move, got %d", state.StateID)
	}
}
