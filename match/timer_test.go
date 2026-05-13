package match

import (
	"testing"
	"time"

	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/storage"
)

// timeoutGame is the same shape as pingPongGame but with a tiny
// TimeBudget so we can verify AutoExpire fires.
func timeoutGame() *core.Game {
	type state struct{ Pings int }
	return &core.Game{
		Name:       "timeout",
		MinPlayers: 2,
		MaxPlayers: 2,
		Setup:      func(_ core.Ctx, _ any) core.G { return &state{} },
		Moves: map[string]any{
			"ping": core.MoveFn(func(mc *core.MoveContext, _ ...any) (core.G, error) {
				s := mc.G.(*state)
				return &state{Pings: s.Pings + 1}, nil
			}),
		},
		Turn: &core.TurnConfig{
			MinMoves:   1,
			MaxMoves:   1,
			TimeBudget: 50 * time.Millisecond,
		},
	}
}

func TestAutoExpireFiresOnTimeout(t *testing.T) {
	m := NewManager(storage.NewMemory())
	m.MustRegister(timeoutGame())
	id, _ := m.Create("timeout", CreateOptions{})

	// Both seats filled so moves would be legal.
	_, _ = m.Join(id, "alice", JoinOptions{})
	_, _ = m.Join(id, "bob", JoinOptions{})

	// Read initial turn.
	mm, _ := m.State(id)
	startTurn := mm.State.Ctx.Turn

	// Wait past the budget; AutoExpire should have run via the AfterFunc.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		mm, _ := m.State(id)
		if mm.State.Ctx.Turn > startTurn {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("expected AutoExpire to advance turn from %d, still at %d", startTurn, mm.State.Ctx.Turn)
}

func TestAutoExpireStaleNoOp(t *testing.T) {
	m := NewManager(storage.NewMemory())
	g := timeoutGame()
	g.Turn.TimeBudget = time.Hour // never fire on its own
	m.MustRegister(g)
	id, _ := m.Create("timeout", CreateOptions{})
	_, _ = m.Join(id, "alice", JoinOptions{})
	_, _ = m.Join(id, "bob", JoinOptions{})

	mm, _ := m.State(id)
	// Call with the wrong expected turn — should no-op.
	if err := m.AutoExpire(id, mm.State.Ctx.Turn+99); err != nil {
		t.Fatalf("AutoExpire: %v", err)
	}
	after, _ := m.State(id)
	if after.State.Ctx.Turn != mm.State.Ctx.Turn {
		t.Fatalf("stale AutoExpire should be a no-op; turn changed %d -> %d",
			mm.State.Ctx.Turn, after.State.Ctx.Turn)
	}
}
