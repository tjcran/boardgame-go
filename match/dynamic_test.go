package match

import (
	"testing"

	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/storage"
)

// dynamicGame: AllowDynamicPlayers so Join can add seats mid-match.
// Matches BGIO issue #884 / #1102.
func dynamicGame() *core.Game {
	return &core.Game{
		Name:                "dyn",
		MinPlayers:          1,
		MaxPlayers:          4,
		AllowDynamicPlayers: true,
		Setup:               func(_ core.Ctx, _ any) core.G { return struct{}{} },
		Moves: map[string]any{
			"noop": core.MoveFn(func(mc *core.MoveContext, _ ...any) (core.G, error) {
				return mc.G, nil
			}),
		},
		Turn: &core.TurnConfig{MinMoves: 1, MaxMoves: 1},
	}
}

func TestBGIO884_DynamicJoinAddsSeats(t *testing.T) {
	m := NewManager(storage.NewMemory())
	m.MustRegister(dynamicGame())
	id, err := m.Create("dyn", CreateOptions{NumPlayers: 1})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Seat the first player at index 0.
	first, err := m.Join(id, "alice", JoinOptions{})
	if err != nil {
		t.Fatalf("first join: %v", err)
	}
	if first.Seat != "0" {
		t.Fatalf("expected seat 0, got %s", first.Seat)
	}

	// Mid-match: another player joins. With AllowDynamicPlayers, the
	// match grows by a seat.
	second, err := m.Join(id, "bob", JoinOptions{})
	if err != nil {
		t.Fatalf("second join: %v", err)
	}
	if second.Seat != "1" {
		t.Fatalf("expected dynamic seat 1, got %s", second.Seat)
	}

	mm, _ := m.State(id)
	if mm.State.Ctx.NumPlayers != 2 {
		t.Fatalf("expected NumPlayers=2 after growth, got %d", mm.State.Ctx.NumPlayers)
	}
	if len(mm.State.Ctx.PlayOrder) != 2 || mm.State.Ctx.PlayOrder[1] != "1" {
		t.Fatalf("expected PlayOrder appended, got %v", mm.State.Ctx.PlayOrder)
	}
}

func TestBGIO884_DynamicJoinHonoursMaxPlayers(t *testing.T) {
	m := NewManager(storage.NewMemory())
	g := dynamicGame()
	g.MaxPlayers = 2
	m.MustRegister(g)
	id, _ := m.Create("dyn", CreateOptions{NumPlayers: 1})
	_, _ = m.Join(id, "a", JoinOptions{})
	_, err := m.Join(id, "b", JoinOptions{}) // seat 1 — grows from 1 to 2
	if err != nil {
		t.Fatalf("second join: %v", err)
	}
	_, err = m.Join(id, "c", JoinOptions{}) // seat 2 — would exceed MaxPlayers=2
	if err == nil {
		t.Fatal("expected MaxPlayers cap to reject the third dynamic join")
	}
}
