// Package match wraps the pure core engine with match lifecycle: creating
// matches, seating players, applying moves under a per-match lock, and
// broadcasting updates to subscribers. The HTTP/WS layer talks to a Manager
// and never reaches into core directly.
package match

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/storage"
)

// Errors returned by Manager. They're surfaced through the HTTP layer with
// appropriate status codes (see server package).
var (
	ErrUnknownGame  = errors.New("unknown game")
	ErrSeatTaken    = errors.New("seat already taken")
	ErrNoSeatsLeft  = errors.New("no seats available")
	ErrUnknownSeat  = errors.New("player not seated in this match")
	ErrSeatRequired = errors.New("game not yet ready (seats unfilled)")
)

// Subscriber receives state pushes for a single match. The WebSocket transport
// implements this; tests can implement it directly.
type Subscriber interface {
	// Send is called with the new state after every successful move. Send
	// must not block — the manager calls it under the match lock.
	Send(state core.State)
	// PlayerID identifies which seat (if any) this subscriber represents.
	// Returning "" makes the subscriber a spectator and receives the
	// spectator-redacted view from Game.PlayerView. The match manager
	// calls this once per push to compute the right redacted state.
	PlayerID() string
}

// Manager owns the live registry of games and the lock that serialises
// per-match writes. It's safe for concurrent use from many HTTP handlers.
type Manager struct {
	store storage.Storage

	mu    sync.Mutex
	games map[string]*core.Game

	// per-match write locks. We don't hold the manager mu while applying a
	// move; that would serialise across matches unnecessarily.
	matchLocks sync.Map // matchID -> *sync.Mutex

	subsMu sync.Mutex
	subs   map[string]map[Subscriber]struct{}

	// now lets tests inject a clock; production uses time.Now.
	now func() time.Time
}

// NewManager builds a manager backed by the given storage. Register games
// with Register before creating matches.
func NewManager(store storage.Storage) *Manager {
	return &Manager{
		store: store,
		games: map[string]*core.Game{},
		subs:  map[string]map[Subscriber]struct{}{},
		now:   time.Now,
	}
}

// Register makes a game available to clients. Call once at server startup
// per game.
func (m *Manager) Register(g *core.Game) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.games[g.Name] = g
}

// Game returns the registered Game for name, or nil if unknown.
func (m *Manager) Game(name string) *core.Game {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.games[name]
}

// Create starts a fresh match. numPlayers may be 0 to use the game's
// default. setupData is the optional game-specific payload (see
// Game.Setup). Returns the new match ID.
func (m *Manager) Create(gameName string, numPlayers int, setupData any) (string, error) {
	g := m.Game(gameName)
	if g == nil {
		return "", fmt.Errorf("%w: %s", ErrUnknownGame, gameName)
	}
	if g.ValidateSetupData != nil {
		if msg := g.ValidateSetupData(setupData, g.PlayerCount(numPlayers)); msg != "" {
			return "", fmt.Errorf("invalid setupData: %s", msg)
		}
	}
	id := newID()
	match := &storage.Match{
		ID:        id,
		GameName:  gameName,
		State:     core.NewMatch(g, numPlayers, setupData),
		SetupData: setupData,
		CreatedAt: m.now().Unix(),
	}
	if err := m.store.Create(match); err != nil {
		return "", err
	}
	return id, nil
}

// JoinResult is what Join returns: the player's stable ID + the seat they
// were assigned (matches ctx.PlayOrder entries).
type JoinResult struct {
	PlayerID string `json:"playerID"`
	Seat     string `json:"seat"`
}

// Join seats a player. If playerID is empty, one is generated. If seat is
// empty, the first free seat is taken. Returns the assigned (PlayerID, Seat).
//
// Re-joining with an already-seated playerID is idempotent — useful for
// reconnects.
func (m *Manager) Join(matchID, playerID, name, seat string) (JoinResult, error) {
	unlock := m.lockMatch(matchID)
	defer unlock()

	match, err := m.store.Get(matchID)
	if err != nil {
		return JoinResult{}, err
	}

	if playerID == "" {
		playerID = newID()
	}

	// idempotent reconnect
	for _, p := range match.Players {
		if p.ID == playerID {
			return JoinResult{PlayerID: p.ID, Seat: p.Seat}, nil
		}
	}

	taken := map[string]bool{}
	for _, p := range match.Players {
		taken[p.Seat] = true
	}

	if seat != "" {
		if taken[seat] {
			return JoinResult{}, ErrSeatTaken
		}
	} else {
		for i := 0; i < match.State.Ctx.NumPlayers; i++ {
			s := strconv.Itoa(i)
			if !taken[s] {
				seat = s
				break
			}
		}
		if seat == "" {
			return JoinResult{}, ErrNoSeatsLeft
		}
	}

	match.Players = append(match.Players, storage.Player{
		ID: playerID, Name: name, Seat: seat,
	})
	if err := m.store.Update(match); err != nil {
		return JoinResult{}, err
	}
	return JoinResult{PlayerID: playerID, Seat: seat}, nil
}

// State returns a snapshot of the match's state.
func (m *Manager) State(matchID string) (*storage.Match, error) {
	return m.store.Get(matchID)
}

// List returns all matches for a given game (empty gameName lists everything).
func (m *Manager) List(gameName string) ([]*storage.Match, error) {
	return m.store.List(gameName)
}

// Move resolves the player's seat and applies the move through the core
// reducer. On success the new state is persisted and broadcast.
func (m *Manager) Move(matchID, playerID, moveName string, args []any) (core.State, error) {
	unlock := m.lockMatch(matchID)
	defer unlock()

	match, err := m.store.Get(matchID)
	if err != nil {
		return core.State{}, err
	}
	g := m.Game(match.GameName)
	if g == nil {
		return core.State{}, fmt.Errorf("%w: %s", ErrUnknownGame, match.GameName)
	}

	seat := ""
	for _, p := range match.Players {
		if p.ID == playerID {
			seat = p.Seat
			break
		}
	}
	if seat == "" {
		return core.State{}, ErrUnknownSeat
	}

	if len(match.Players) < match.State.Ctx.NumPlayers {
		return core.State{}, ErrSeatRequired
	}

	next, err := core.Apply(g, match.State, core.MoveRequest{
		PlayerID: seat,
		Move:     moveName,
		Args:     args,
	})
	if err != nil {
		return core.State{}, err
	}
	match.State = next
	if err := m.store.Update(match); err != nil {
		return core.State{}, err
	}

	m.broadcast(matchID, next)
	return next, nil
}

// Subscribe registers s to receive state pushes for matchID. Returns an
// unsubscribe func — caller must invoke it on disconnect.
func (m *Manager) Subscribe(matchID string, s Subscriber) func() {
	m.subsMu.Lock()
	defer m.subsMu.Unlock()
	set, ok := m.subs[matchID]
	if !ok {
		set = map[Subscriber]struct{}{}
		m.subs[matchID] = set
	}
	set[s] = struct{}{}
	return func() {
		m.subsMu.Lock()
		defer m.subsMu.Unlock()
		if set, ok := m.subs[matchID]; ok {
			delete(set, s)
			if len(set) == 0 {
				delete(m.subs, matchID)
			}
		}
	}
}

func (m *Manager) broadcast(matchID string, state core.State) {
	m.subsMu.Lock()
	subs := make([]Subscriber, 0, len(m.subs[matchID]))
	for s := range m.subs[matchID] {
		subs = append(subs, s)
	}
	m.subsMu.Unlock()

	match, err := m.store.Get(matchID)
	if err != nil {
		return
	}
	game := m.Game(match.GameName)
	if game == nil {
		return
	}
	// Cache per-seat views so concurrent subscribers for the same seat
	// share one redacted state object.
	views := map[string]core.State{}
	for _, s := range subs {
		pid := s.PlayerID()
		v, ok := views[pid]
		if !ok {
			v = core.PlayerView(game, state, pid)
			views[pid] = v
		}
		s.Send(v)
	}
}

// lockMatch returns a per-match exclusive lock. We do this so two concurrent
// moves against the same match serialise, but different matches don't.
func (m *Manager) lockMatch(matchID string) func() {
	v, _ := m.matchLocks.LoadOrStore(matchID, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

func newID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
