package match

import (
	"errors"
	"testing"

	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/storage"
)

// pingPongGame: two players, each takes turns calling "ping". After 4 pings
// player 0 wins. Lets us exercise create/join/move without coupling to
// tic-tac-toe.
func pingPongGame() *core.Game {
	type state struct{ Pings int }
	return &core.Game{
		Name:       "pp",
		MinPlayers: 2,
		MaxPlayers: 2,
		Setup:      func(_ core.Ctx, _ any) core.G { return &state{} },
		Moves: map[string]any{
			"ping": core.MoveFn(func(mc *core.MoveContext, _ ...any) (core.G, error) {
				s := mc.G.(*state)
				return &state{Pings: s.Pings + 1}, nil
			}),
		},
		EndIf: func(mc *core.MoveContext) any {
			if mc.G.(*state).Pings >= 4 {
				return map[string]any{"winner": "0"}
			}
			return nil
		},
		Turn: &core.TurnConfig{MinMoves: 1, MaxMoves: 1},
	}
}

func newTestManager(t *testing.T) (*Manager, string) {
	t.Helper()
	m := NewManager(storage.NewMemory())
	m.Register(pingPongGame())
	id, err := m.Create("pp", 0, nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	return m, id
}

func TestCreateRejectsUnknownGame(t *testing.T) {
	m := NewManager(storage.NewMemory())
	_, err := m.Create("nope", 0, nil)
	if !errors.Is(err, ErrUnknownGame) {
		t.Fatalf("expected ErrUnknownGame, got %v", err)
	}
}

func TestJoinAssignsSequentialSeats(t *testing.T) {
	m, id := newTestManager(t)
	a, err := m.Join(id, "", "alice", "")
	if err != nil {
		t.Fatalf("join: %v", err)
	}
	if a.Seat != "0" {
		t.Fatalf("expected seat 0, got %s", a.Seat)
	}
	b, err := m.Join(id, "", "bob", "")
	if err != nil {
		t.Fatalf("join: %v", err)
	}
	if b.Seat != "1" {
		t.Fatalf("expected seat 1, got %s", b.Seat)
	}
}

func TestJoinIsIdempotent(t *testing.T) {
	m, id := newTestManager(t)
	a, _ := m.Join(id, "alice", "alice", "")
	a2, err := m.Join(id, "alice", "alice", "")
	if err != nil || a2.Seat != a.Seat {
		t.Fatalf("reconnect should be idempotent, got %+v err=%v", a2, err)
	}
}

func TestMoveRequiresSeatedPlayer(t *testing.T) {
	m, id := newTestManager(t)
	m.Join(id, "alice", "alice", "")
	m.Join(id, "bob", "bob", "")
	_, err := m.Move(id, "carol", "ping", nil)
	if !errors.Is(err, ErrUnknownSeat) {
		t.Fatalf("expected ErrUnknownSeat, got %v", err)
	}
}

func TestMoveRejectedUntilAllSeatsFilled(t *testing.T) {
	m, id := newTestManager(t)
	m.Join(id, "alice", "alice", "")
	_, err := m.Move(id, "alice", "ping", nil)
	if !errors.Is(err, ErrSeatRequired) {
		t.Fatalf("expected ErrSeatRequired, got %v", err)
	}
}

func TestMovePlaysThroughEnd(t *testing.T) {
	m, id := newTestManager(t)
	m.Join(id, "alice", "alice", "") // seat 0
	m.Join(id, "bob", "bob", "")     // seat 1

	turns := []string{"alice", "bob", "alice", "bob"}
	var last core.State
	for i, who := range turns {
		st, err := m.Move(id, who, "ping", nil)
		if err != nil {
			t.Fatalf("move %d: %v", i, err)
		}
		last = st
	}
	if last.Ctx.Gameover == nil {
		t.Fatalf("expected gameover, got %+v", last.Ctx)
	}
	if m, _ := last.Ctx.Gameover.(map[string]any); m["winner"] != "0" {
		t.Fatalf("expected winner=0, got %v", last.Ctx.Gameover)
	}
}

type captureSub struct{ states []core.State }

func (c *captureSub) Send(s core.State) { c.states = append(c.states, s) }

func TestSubscribersReceiveBroadcasts(t *testing.T) {
	m, id := newTestManager(t)
	m.Join(id, "alice", "alice", "")
	m.Join(id, "bob", "bob", "")
	c := &captureSub{}
	unsub := m.Subscribe(id, c)
	defer unsub()
	if _, err := m.Move(id, "alice", "ping", nil); err != nil {
		t.Fatalf("move: %v", err)
	}
	if len(c.states) != 1 {
		t.Fatalf("expected 1 broadcast, got %d", len(c.states))
	}
}
