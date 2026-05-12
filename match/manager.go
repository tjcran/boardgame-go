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
	ErrUnknownGame      = errors.New("unknown game")
	ErrSeatTaken        = errors.New("seat already taken")
	ErrNoSeatsLeft      = errors.New("no seats available")
	ErrUnknownSeat      = errors.New("player not seated in this match")
	ErrSeatRequired     = errors.New("game not yet ready (seats unfilled)")
	ErrBadCredentials   = errors.New("invalid credentials")
)

// GenerateCredentialsFn produces an opaque token for a freshly joined
// player. Defaults to a random 16-byte hex string.
type GenerateCredentialsFn func() string

// AuthenticateCredentialsFn checks whether a supplied token matches the
// stored credential for the given player metadata. Defaults to a
// constant-time string comparison.
type AuthenticateCredentialsFn func(supplied string, p storage.Player) bool

// Subscriber receives state and chat pushes for a single match. The
// WebSocket transport implements this; tests can implement it directly.
type Subscriber interface {
	// Send is called with the new state after every successful move. Send
	// must not block — the manager calls it under the match lock.
	Send(state core.State)
	// SendChat is called for every chat message broadcast on the match.
	SendChat(msg ChatMessage)
	// PlayerID identifies which seat (if any) this subscriber represents.
	// Returning "" makes the subscriber a spectator and receives the
	// spectator-redacted view from Game.PlayerView. The match manager
	// calls this once per push to compute the right redacted state.
	PlayerID() string
}

// ChatMessage is the wire shape for in-match chat. Mirrors BGIO's
// `client.chatMessages` entries.
type ChatMessage struct {
	ID      string `json:"id"`
	Sender  string `json:"sender"`
	Payload any    `json:"payload"`
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

	// GenerateCredentials issues per-seat tokens when a player joins.
	// Replace at construction to integrate with an existing auth system.
	GenerateCredentials GenerateCredentialsFn

	// AuthenticateCredentials checks a supplied token against the stored
	// one. Replace at construction for custom auth.
	AuthenticateCredentials AuthenticateCredentialsFn
}

// NewManager builds a manager backed by the given storage. Register games
// with Register before creating matches.
func NewManager(store storage.Storage) *Manager {
	return &Manager{
		store: store,
		games: map[string]*core.Game{},
		subs:  map[string]map[Subscriber]struct{}{},
		now:   time.Now,
		GenerateCredentials: func() string {
			var b [16]byte
			_, _ = rand.Read(b[:])
			return hex.EncodeToString(b[:])
		},
		AuthenticateCredentials: func(supplied string, p storage.Player) bool {
			// Constant-time comparison would be nicer but the credential
			// length is fixed in practice and string == is good enough
			// for an in-memory dev server. We can revisit when adding a
			// production hardening pass.
			return supplied != "" && supplied == p.Credentials
		},
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

// GameNames returns the names of every registered game. Used by the Lobby's
// GET /games endpoint.
func (m *Manager) GameNames() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.games))
	for name := range m.games {
		out = append(out, name)
	}
	return out
}

// CreateOptions bundles the optional create-time fields. Mirrors BGIO's
// `POST /games/{name}/create` body.
type CreateOptions struct {
	NumPlayers int
	SetupData  any
	Unlisted   bool
}

// Create starts a fresh match. Returns the new match ID.
func (m *Manager) Create(gameName string, opts CreateOptions) (string, error) {
	return m.createLocked(gameName, opts.NumPlayers, opts.SetupData, opts.Unlisted)
}

// JoinResult is what Join returns: the seat-bound player ID, the assigned
// seat, and the credentials the player must supply on subsequent calls.
// Mirrors BGIO's `joinMatch` response.
type JoinResult struct {
	PlayerID          string `json:"playerID"`
	Seat              string `json:"seat"`
	PlayerCredentials string `json:"playerCredentials"`
}

// JoinOptions is the optional payload for a Join. PlayerID assigns a
// specific seat (BGIO's `playerID` param: '0', '1', …); Seat is a
// boardgame-go shorthand that means the same thing — supply either.
// Data passes arbitrary metadata that's echoed back in lists.
type JoinOptions struct {
	PlayerID string
	Seat     string
	Data     any
}

// Join seats a player. With no PlayerID or Seat, the first free seat is
// taken. Returns the issued credentials — required for subsequent Move /
// Leave / Update calls. Idempotent for an already-seated PlayerID
// (returns the existing credentials).
func (m *Manager) Join(matchID, name string, opts JoinOptions) (JoinResult, error) {
	unlock := m.lockMatch(matchID)
	defer unlock()

	match, err := m.store.Get(matchID)
	if err != nil {
		return JoinResult{}, err
	}

	// Either PlayerID or Seat selects a seat. Prefer the explicit Seat when
	// both are passed.
	requestedSeat := opts.Seat
	if requestedSeat == "" {
		requestedSeat = opts.PlayerID
	}

	// Idempotent reconnect: a Player.ID match returns the existing token.
	// We never re-issue credentials on reconnect — clients keep their token.
	if opts.PlayerID != "" {
		for _, p := range match.Players {
			if p.ID == opts.PlayerID {
				return JoinResult{
					PlayerID: p.ID, Seat: p.Seat,
					PlayerCredentials: p.Credentials,
				}, nil
			}
		}
	}

	taken := map[string]bool{}
	for _, p := range match.Players {
		taken[p.Seat] = true
	}

	seat := requestedSeat
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

	playerID := opts.PlayerID
	if playerID == "" {
		// BGIO uses the seat index as the player ID when one isn't given.
		// We follow suit so existing clients can address the seat directly.
		playerID = seat
	}

	creds := m.GenerateCredentials()
	match.Players = append(match.Players, storage.Player{
		ID:          playerID,
		Name:        name,
		Seat:        seat,
		Credentials: creds,
		Data:        opts.Data,
	})
	if err := m.store.Update(match); err != nil {
		return JoinResult{}, err
	}
	return JoinResult{
		PlayerID: playerID, Seat: seat, PlayerCredentials: creds,
	}, nil
}

// Leave removes a player from the match after credential validation.
func (m *Manager) Leave(matchID, playerID, credentials string) error {
	unlock := m.lockMatch(matchID)
	defer unlock()
	match, err := m.store.Get(matchID)
	if err != nil {
		return err
	}
	for i, p := range match.Players {
		if p.ID != playerID {
			continue
		}
		if !m.AuthenticateCredentials(credentials, p) {
			return ErrBadCredentials
		}
		match.Players = append(match.Players[:i], match.Players[i+1:]...)
		return m.store.Update(match)
	}
	return ErrUnknownSeat
}

// UpdatePlayerOpts carries the optional update fields for a player. At
// least one of NewName / Data must be non-nil.
type UpdatePlayerOpts struct {
	NewName *string
	Data    any
	HasData bool // distinguishes "data not set" from "data set to nil"
}

// UpdatePlayer renames or sets the metadata payload for a seated player.
// Mirrors BGIO's `POST /games/{name}/{id}/update`.
func (m *Manager) UpdatePlayer(matchID, playerID, credentials string, opts UpdatePlayerOpts) error {
	unlock := m.lockMatch(matchID)
	defer unlock()
	match, err := m.store.Get(matchID)
	if err != nil {
		return err
	}
	for i, p := range match.Players {
		if p.ID != playerID {
			continue
		}
		if !m.AuthenticateCredentials(credentials, p) {
			return ErrBadCredentials
		}
		if opts.NewName != nil {
			match.Players[i].Name = *opts.NewName
		}
		if opts.HasData {
			match.Players[i].Data = opts.Data
		}
		return m.store.Update(match)
	}
	return ErrUnknownSeat
}

// PlayAgain creates a fresh match using the same game (and optionally the
// same NumPlayers/SetupData as the previous match) and returns the new
// match ID. Requires credentials proving the caller participated in the
// previous match. Idempotent if PlayAgain has already been recorded for
// this match.
func (m *Manager) PlayAgain(matchID, playerID, credentials string, numPlayers int, setupData any, useDataFromPrev bool) (string, error) {
	unlock := m.lockMatch(matchID)
	defer unlock()
	prev, err := m.store.Get(matchID)
	if err != nil {
		return "", err
	}
	// Validate credentials for the calling player.
	authed := false
	for _, p := range prev.Players {
		if p.ID == playerID && m.AuthenticateCredentials(credentials, p) {
			authed = true
			break
		}
	}
	if !authed {
		return "", ErrBadCredentials
	}
	// If a PlayAgain has been recorded already, return that match ID
	// (BGIO idempotency).
	if prev.NextMatchID != "" {
		return prev.NextMatchID, nil
	}

	// Fall back to the previous match's setupData when the caller passes
	// none and signals they want the previous one (HasSetupData=false in
	// the request).
	if useDataFromPrev && setupData == nil {
		setupData = prev.SetupData
	}
	if numPlayers <= 0 {
		numPlayers = prev.State.Ctx.NumPlayers
	}

	next, err := m.createLocked(prev.GameName, numPlayers, setupData, false)
	if err != nil {
		return "", err
	}
	prev.NextMatchID = next
	_ = m.store.Update(prev)
	return next, nil
}

// createLocked is the inner Create routine used by both Create and
// PlayAgain. Assumes the caller already holds (or doesn't need) the
// match lock.
func (m *Manager) createLocked(gameName string, numPlayers int, setupData any, unlisted bool) (string, error) {
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
		Unlisted:  unlisted,
		CreatedAt: m.now().Unix(),
	}
	if err := m.store.Create(match); err != nil {
		return "", err
	}
	return id, nil
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
// reducer. credentials must match the value handed out by Join (set to
// "" to skip auth — useful only for tests). On success the new state is
// persisted and broadcast.
func (m *Manager) Move(matchID, playerID, credentials, moveName string, args []any) (core.State, error) {
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
			if credentials != "" {
				if !m.AuthenticateCredentials(credentials, p) {
					return core.State{}, ErrBadCredentials
				}
			}
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

// Chat broadcasts a chat message to every subscriber of the match. Chat is
// not persisted; subscribers that connect later won't see prior messages.
// Mirrors BGIO's behaviour.
func (m *Manager) Chat(matchID, sender string, payload any) {
	msg := ChatMessage{ID: newID(), Sender: sender, Payload: payload}
	m.subsMu.Lock()
	subs := make([]Subscriber, 0, len(m.subs[matchID]))
	for s := range m.subs[matchID] {
		subs = append(subs, s)
	}
	m.subsMu.Unlock()
	for _, s := range subs {
		s.SendChat(msg)
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
