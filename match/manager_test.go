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
	id, err := m.Create("pp", CreateOptions{})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	return m, id
}

// joinHelper is a thin wrapper that returns (playerID, creds) so tests
// don't have to keep destructuring JoinResult. Seat is left automatic so
// BGIO conventions (playerID = seat index) flow through.
func joinHelper(t *testing.T, m *Manager, id, name string) (string, string) {
	t.Helper()
	r, err := m.Join(id, name, JoinOptions{})
	if err != nil {
		t.Fatalf("join %s: %v", name, err)
	}
	return r.PlayerID, r.PlayerCredentials
}

func TestCreateRejectsUnknownGame(t *testing.T) {
	m := NewManager(storage.NewMemory())
	_, err := m.Create("nope", CreateOptions{})
	if !errors.Is(err, ErrUnknownGame) {
		t.Fatalf("expected ErrUnknownGame, got %v", err)
	}
}

func TestJoinAssignsSequentialSeats(t *testing.T) {
	m, id := newTestManager(t)
	a, err := m.Join(id, "alice", JoinOptions{})
	if err != nil {
		t.Fatalf("join: %v", err)
	}
	if a.Seat != "0" {
		t.Fatalf("expected seat 0, got %s", a.Seat)
	}
	b, err := m.Join(id, "bob", JoinOptions{})
	if err != nil {
		t.Fatalf("join: %v", err)
	}
	if b.Seat != "1" {
		t.Fatalf("expected seat 1, got %s", b.Seat)
	}
	if a.PlayerCredentials == "" || b.PlayerCredentials == "" {
		t.Fatal("expected credentials issued on join")
	}
	if a.PlayerCredentials == b.PlayerCredentials {
		t.Fatal("expected distinct credentials per seat")
	}
}

func TestJoinIsIdempotent(t *testing.T) {
	m, id := newTestManager(t)
	a, _ := m.Join(id, "alice", JoinOptions{PlayerID: "alice"})
	a2, err := m.Join(id, "alice", JoinOptions{PlayerID: "alice"})
	if err != nil || a2.Seat != a.Seat || a2.PlayerCredentials != a.PlayerCredentials {
		t.Fatalf("reconnect should be idempotent, got %+v err=%v", a2, err)
	}
}

func TestMoveRequiresSeatedPlayer(t *testing.T) {
	m, id := newTestManager(t)
	_, _ = joinHelper(t, m, id, "alice")
	_, _ = joinHelper(t, m, id, "bob")
	_, err := m.Move(id, "carol", "", "ping", nil)
	if !errors.Is(err, ErrUnknownSeat) {
		t.Fatalf("expected ErrUnknownSeat, got %v", err)
	}
}

func TestMoveRejectsBadCredentials(t *testing.T) {
	m, id := newTestManager(t)
	pid, _ := joinHelper(t, m, id, "alice")
	_, _ = joinHelper(t, m, id, "bob")
	_, err := m.Move(id, pid, "wrong-token", "ping", nil)
	if !errors.Is(err, ErrBadCredentials) {
		t.Fatalf("expected ErrBadCredentials, got %v", err)
	}
}

func TestMoveRejectedUntilAllSeatsFilled(t *testing.T) {
	m, id := newTestManager(t)
	pid, creds := joinHelper(t, m, id, "alice")
	_, err := m.Move(id, pid, creds, "ping", nil)
	if !errors.Is(err, ErrSeatRequired) {
		t.Fatalf("expected ErrSeatRequired, got %v", err)
	}
}

func TestMovePlaysThroughEnd(t *testing.T) {
	m, id := newTestManager(t)
	aliceID, aliceCreds := joinHelper(t, m, id, "alice") // seat 0
	bobID, bobCreds := joinHelper(t, m, id, "bob")         // seat 1

	type turnSpec struct {
		pid, creds string
	}
	turns := []turnSpec{
		{aliceID, aliceCreds}, {bobID, bobCreds},
		{aliceID, aliceCreds}, {bobID, bobCreds},
	}
	var last core.State
	for i, t2 := range turns {
		st, err := m.Move(id, t2.pid, t2.creds, "ping", nil)
		if err != nil {
			t.Fatalf("move %d: %v", i, err)
		}
		last = st
	}
	if last.Ctx.Gameover == nil {
		t.Fatalf("expected gameover, got %+v", last.Ctx)
	}
	if m2, _ := last.Ctx.Gameover.(map[string]any); m2["winner"] != "0" {
		t.Fatalf("expected winner=0, got %v", last.Ctx.Gameover)
	}
}

type captureSub struct {
	states     []core.State
	chats      []ChatMessage
	matchDatas [][]storage.Player
	pid        string
}

func (c *captureSub) Send(s core.State)                { c.states = append(c.states, s) }
func (c *captureSub) SendChat(msg ChatMessage)         { c.chats = append(c.chats, msg) }
func (c *captureSub) SendMatchData(p []storage.Player) { c.matchDatas = append(c.matchDatas, p) }
func (c *captureSub) PlayerID() string                 { return c.pid }

func TestSubscribersReceiveBroadcasts(t *testing.T) {
	m, id := newTestManager(t)
	pid, creds := joinHelper(t, m, id, "alice")
	_, _ = joinHelper(t, m, id, "bob")
	c := &captureSub{}
	unsub := m.Subscribe(id, c)
	defer unsub()
	if _, err := m.Move(id, pid, creds, "ping", nil); err != nil {
		t.Fatalf("move: %v", err)
	}
	if len(c.states) != 1 {
		t.Fatalf("expected 1 broadcast, got %d", len(c.states))
	}
}

func TestSubscribersReceiveChat(t *testing.T) {
	m, id := newTestManager(t)
	c := &captureSub{}
	unsub := m.Subscribe(id, c)
	defer unsub()
	m.Chat(id, "alice", "hi")
	if len(c.chats) != 1 {
		t.Fatalf("expected 1 chat message, got %d", len(c.chats))
	}
	if c.chats[0].Sender != "alice" || c.chats[0].Payload != "hi" {
		t.Fatalf("unexpected chat: %+v", c.chats[0])
	}
}

func TestLeaveRemovesPlayer(t *testing.T) {
	m, id := newTestManager(t)
	pid, creds := joinHelper(t, m, id, "alice")
	if err := m.Leave(id, pid, creds); err != nil {
		t.Fatalf("leave: %v", err)
	}
	mm, _ := m.State(id)
	if len(mm.Players) != 0 {
		t.Fatalf("expected no players after leave, got %d", len(mm.Players))
	}
}

func TestPlayAgainCreatesNewMatch(t *testing.T) {
	m, id := newTestManager(t)
	pid, creds := joinHelper(t, m, id, "alice")
	_, _ = joinHelper(t, m, id, "bob")
	nextID, err := m.PlayAgain(id, pid, creds, 0, nil, true)
	if err != nil {
		t.Fatalf("playAgain: %v", err)
	}
	if nextID == "" || nextID == id {
		t.Fatalf("expected fresh match id, got %q (orig %q)", nextID, id)
	}
	// Idempotent: a second call returns the same nextMatchID.
	again, err := m.PlayAgain(id, pid, creds, 0, nil, true)
	if err != nil {
		t.Fatalf("playAgain again: %v", err)
	}
	if again != nextID {
		t.Fatalf("expected idempotent PlayAgain, got %q vs %q", again, nextID)
	}
}

func TestGameBeforePersistScrubsStorageOnly(t *testing.T) {
	type myG struct {
		Persistent string
		Transient  string
	}
	game := &core.Game{
		Name: "before-persist-test", MinPlayers: 2, MaxPlayers: 2,
		Setup: func(_ core.Ctx, _ any) core.G { return &myG{} },
		Moves: map[string]any{
			"act": core.MoveFn(func(mc *core.MoveContext, _ ...any) (core.G, error) {
				return &myG{Persistent: "keep", Transient: "drop"}, nil
			}),
		},
		BeforePersist: func(s core.State) core.State {
			if g, ok := s.G.(*myG); ok {
				clone := *g
				clone.Transient = ""
				s.G = &clone
			}
			return s
		},
		Turn: &core.TurnConfig{MinMoves: 1, MaxMoves: 1},
	}

	store := storage.NewMemory()
	m := NewManager(store)
	m.MustRegister(game)

	var observedState core.State
	m.OnLifecycleKind(LifecycleMatchMoved, func(ev LifecycleEvent) {
		observedState = ev.State
	})

	id, _ := m.Create("before-persist-test", CreateOptions{})
	alice, _ := m.Join(id, "alice", JoinOptions{})
	_, _ = m.Join(id, "bob", JoinOptions{})

	if _, err := m.Move(id, alice.PlayerID, alice.PlayerCredentials, "act", nil); err != nil {
		t.Fatalf("Move: %v", err)
	}

	// (a) Storage should have the trimmed state — Transient empty.
	stored, err := store.Get(id)
	if err != nil {
		t.Fatalf("store.Get: %v", err)
	}
	storedG, ok := stored.State.G.(*myG)
	if !ok {
		t.Fatalf("stored state G is not *myG: %T", stored.State.G)
	}
	if storedG.Transient != "" {
		t.Errorf("storage Transient = %q, want empty (BeforePersist should have run)", storedG.Transient)
	}
	if storedG.Persistent != "keep" {
		t.Errorf("storage Persistent = %q, want \"keep\"", storedG.Persistent)
	}

	// (b) Lifecycle observer should have seen the full state — Transient="drop".
	if observedState.G == nil {
		t.Fatalf("lifecycle event State.G is nil — observer never fired or saw zero state")
	}
	obsG, ok := observedState.G.(*myG)
	if !ok {
		t.Fatalf("observed state G is not *myG: %T", observedState.G)
	}
	if obsG.Transient != "drop" {
		t.Errorf("lifecycle observer Transient = %q, want \"drop\" (full state)", obsG.Transient)
	}
}

func TestGameBeforePersistNilIsNoOp(t *testing.T) {
	// When Game.BeforePersist is nil, state is persisted unmodified.
	type myG struct{ Val string }
	game := &core.Game{
		Name: "before-persist-nil", MinPlayers: 2, MaxPlayers: 2,
		Setup: func(_ core.Ctx, _ any) core.G { return &myG{} },
		Moves: map[string]any{
			"act": core.MoveFn(func(mc *core.MoveContext, _ ...any) (core.G, error) {
				return &myG{Val: "persisted"}, nil
			}),
		},
		Turn: &core.TurnConfig{MinMoves: 1, MaxMoves: 1},
	}
	store := storage.NewMemory()
	m := NewManager(store)
	m.MustRegister(game)

	id, _ := m.Create("before-persist-nil", CreateOptions{})
	alice, _ := m.Join(id, "alice", JoinOptions{})
	_, _ = m.Join(id, "bob", JoinOptions{})
	if _, err := m.Move(id, alice.PlayerID, alice.PlayerCredentials, "act", nil); err != nil {
		t.Fatalf("Move: %v", err)
	}
	stored, _ := store.Get(id)
	if stored.State.G.(*myG).Val != "persisted" {
		t.Errorf("nil BeforePersist should persist state unmodified, got %q", stored.State.G.(*myG).Val)
	}
}
