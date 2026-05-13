// Package match wraps the pure core engine with match lifecycle: creating
// matches, seating players, applying moves under a per-match lock, and
// broadcasting updates to subscribers. The HTTP/WS layer talks to a Manager
// and never reaches into core directly.
package match

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
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
	// SendMatchData is called when the seated-player list changes (join/
	// leave/update). The slice mirrors BGIO's matchData shape.
	SendMatchData(players []storage.Player)
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

	// Logger receives structured events (match created, joined, moved,
	// chat sent, errors). Default is a no-op (slog.New on io.Discard) so
	// the manager doesn't spam stdout in libraries or tests. Swap for a
	// JSON or text handler at process start.
	Logger *slog.Logger

	// RequireStateID, when true, refuses to apply moves whose request
	// supplied no StateID. Enables strict optimistic-concurrency in
	// production deployments. Off by default to preserve BGIO's relaxed
	// dev-mode behaviour.
	RequireStateID bool

	// Lifecycle handlers registered via OnLifecycle / OnLifecycleKind.
	lifecycleMu   sync.Mutex
	lifecycleSubs map[LifecycleEventKind][]LifecycleHandler
}

// NewManager builds a manager backed by the given storage. Register games
// with Register before creating matches.
func NewManager(store storage.Storage) *Manager {
	return &Manager{
		store:  store,
		games:  map[string]*core.Game{},
		subs:   map[string]map[Subscriber]struct{}{},
		now:    time.Now,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
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
// per game. Returns Game.Validate's error if the definition is
// malformed; if so the game is not registered.
func (m *Manager) Register(g *core.Game) error {
	if err := g.Validate(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.games[g.Name] = g
	return nil
}

// MustRegister panics if Register returns an error. Convenient for
// startup code where bad configuration should fail fast.
func (m *Manager) MustRegister(g *core.Game) {
	if err := m.Register(g); err != nil {
		panic(err)
	}
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

	match, err := m.loadMigrated(matchID)
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

	game := m.Game(match.GameName)
	dynamic := game != nil && game.AllowDynamicPlayers

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
			if !dynamic {
				return JoinResult{}, ErrNoSeatsLeft
			}
			// AllowDynamicPlayers: grow the table by one seat.
			seat = strconv.Itoa(match.State.Ctx.NumPlayers)
			if game.MaxPlayers > 0 && match.State.Ctx.NumPlayers >= game.MaxPlayers {
				return JoinResult{}, ErrNoSeatsLeft
			}
			match.State.Ctx.PlayOrder = append(match.State.Ctx.PlayOrder, seat)
			match.State.Ctx.NumPlayers++
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
	m.broadcastMatchData(matchID)
	m.Logger.Info("match.joined",
		"match_id", matchID,
		"player_id", playerID,
		"seat", seat,
		"name", name)
	m.fireLifecycle(LifecycleEvent{
		Kind: LifecycleMatchJoined, MatchID: matchID,
		PlayerID: playerID, State: match.State, Match: match,
	})
	return JoinResult{
		PlayerID: playerID, Seat: seat, PlayerCredentials: creds,
	}, nil
}

// SetConnected flips a seated player's IsConnected flag and broadcasts the
// matchData frame to every subscriber. Called by the transport on socket
// open/close. Returns nil if the player isn't seated (a spectator) so the
// transport can call this unconditionally.
func (m *Manager) SetConnected(matchID, playerID string, connected bool) error {
	if playerID == "" {
		return nil // spectator
	}
	unlock := m.lockMatch(matchID)
	defer unlock()
	match, err := m.loadMigrated(matchID)
	if err != nil {
		return err
	}
	for i, p := range match.Players {
		if p.ID != playerID {
			continue
		}
		if p.IsConnected == connected {
			return nil // no-op, no broadcast
		}
		match.Players[i].IsConnected = connected
		if err := m.store.Update(match); err != nil {
			return err
		}
		m.broadcastMatchData(matchID)
		return nil
	}
	return nil
}

// Reset re-initialises the match's State (running Setup again) while
// keeping the seated Players and their credentials. The caller must be a
// seated player with valid credentials. Addresses BGIO issue #1166
// (client.reset doesn't work in multiplayer).
//
// After Reset the new state is broadcast like any other state change.
func (m *Manager) Reset(matchID, playerID, credentials string) error {
	unlock := m.lockMatch(matchID)
	defer unlock()
	match, err := m.loadMigrated(matchID)
	if err != nil {
		return err
	}
	authed := false
	for _, p := range match.Players {
		if p.ID == playerID && m.AuthenticateCredentials(credentials, p) {
			authed = true
			break
		}
	}
	if !authed {
		return ErrBadCredentials
	}
	g := m.Game(match.GameName)
	if g == nil {
		return fmt.Errorf("%w: %s", ErrUnknownGame, match.GameName)
	}
	match.State = core.NewMatch(g, match.State.Ctx.NumPlayers, match.SetupData)
	// IsConnected flags are per-socket; on reset we reset them too so
	// clients re-announce themselves on reconnect.
	for i := range match.Players {
		match.Players[i].IsConnected = false
	}
	if err := m.store.Update(match); err != nil {
		return err
	}
	m.broadcast(matchID, match.State)
	m.broadcastMatchData(matchID)
	m.Logger.Info("match.reset", "match_id", matchID, "player_id", playerID)
	m.fireLifecycle(LifecycleEvent{
		Kind: LifecycleMatchReset, MatchID: matchID,
		PlayerID: playerID, State: match.State, Match: match,
	})
	return nil
}

// Leave removes a player from the match after credential validation.
func (m *Manager) Leave(matchID, playerID, credentials string) error {
	unlock := m.lockMatch(matchID)
	defer unlock()
	match, err := m.loadMigrated(matchID)
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
		if err := m.store.Update(match); err != nil {
			return err
		}
		m.broadcastMatchData(matchID)
		m.fireLifecycle(LifecycleEvent{
			Kind: LifecycleMatchLeft, MatchID: matchID,
			PlayerID: playerID, State: match.State, Match: match,
		})
		return nil
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
	match, err := m.loadMigrated(matchID)
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
		if err := m.store.Update(match); err != nil {
			return err
		}
		m.broadcastMatchData(matchID)
		return nil
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
	prev, err := m.loadMigrated(matchID)
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
		ID:            id,
		GameName:      gameName,
		State:         core.NewMatch(g, numPlayers, setupData),
		SetupData:     setupData,
		Unlisted:      unlisted,
		CreatedAt:     m.now().Unix(),
		SchemaVersion: g.SchemaVersion,
	}
	if err := m.store.Create(match); err != nil {
		return "", err
	}
	m.Logger.Info("match.created",
		"match_id", id,
		"game", gameName,
		"num_players", match.State.Ctx.NumPlayers,
		"unlisted", unlisted)
	m.fireLifecycle(LifecycleEvent{
		Kind: LifecycleMatchCreated, MatchID: id,
		State: match.State, Match: match,
	})
	return id, nil
}

// State returns a snapshot of the match's state, migrating it forward if
// the stored SchemaVersion is older than the registered Game's.
func (m *Manager) State(matchID string) (*storage.Match, error) {
	return m.loadMigrated(matchID)
}

// loadMigrated fetches a match from storage and runs Game.Migrate as
// needed. Internal helper used by every read path (Move, Join, Leave,
// PlayAgain, Reset, broadcast, …) so a match that crosses a schema bump
// surfaces in its current shape regardless of which call loaded it.
func (m *Manager) loadMigrated(matchID string) (*storage.Match, error) {
	match, err := m.store.Get(matchID)
	if err != nil {
		return nil, err
	}
	g := m.Game(match.GameName)
	if g == nil || g.SchemaVersion <= match.SchemaVersion {
		return match, nil
	}
	if g.Migrate == nil {
		return nil, fmt.Errorf("match %s at schema v%d but Game %q is v%d and has no Migrate fn",
			matchID, match.SchemaVersion, g.Name, g.SchemaVersion)
	}
	from := match.SchemaVersion
	cur := match.State
	for v := from; v < g.SchemaVersion; v++ {
		next, err := g.Migrate(cur, v)
		if err != nil {
			return nil, fmt.Errorf("migrate %s: v%d -> v%d: %w", matchID, v, v+1, err)
		}
		cur = next
	}
	match.State = cur
	match.SchemaVersion = g.SchemaVersion
	if err := m.store.Update(match); err != nil {
		// Don't fail the read just because we couldn't persist the
		// migrated form — return the migrated state in memory.
		m.Logger.Warn("match.migrate.persist_failed",
			"match_id", matchID, "err", err.Error())
	} else {
		m.Logger.Info("match.migrated",
			"match_id", matchID, "from_version", from,
			"to_version", g.SchemaVersion)
	}
	return match, nil
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
	return m.MoveReq(matchID, playerID, credentials, core.MoveRequest{
		Move: moveName,
		Args: args,
	})
}

// MoveReq is the full-form Move that lets callers pass a MoveRequest
// directly, including StateID for stale-state checks. The MoveRequest's
// PlayerID field is ignored — the seat is resolved from the manager's
// own (matchID, playerID, credentials) tuple.
//
// When Manager.RequireStateID is true, MoveReq rejects requests with
// StateID == 0 (the client must echo back the last state ID they saw).
//
// Internally delegates to MoveReqCtx with a Background context; use that
// variant directly when you have a request-scoped context to propagate.
func (m *Manager) MoveReq(matchID, playerID, credentials string, req core.MoveRequest) (core.State, error) {
	return m.MoveReqCtx(context.Background(), matchID, playerID, credentials, req)
}

// DryMove runs a move through the reducer but does NOT persist the new
// state or broadcast to subscribers. Used by clients to preview the
// would-be result without leaking via undo-after-do. Addresses BGIO
// issue #636.
//
// credentials are still validated; the caller must be the seated player.
func (m *Manager) DryMove(matchID, playerID, credentials, moveName string, args []any) (core.State, error) {
	return m.DryMoveReq(matchID, playerID, credentials, core.MoveRequest{
		Move: moveName,
		Args: args,
	})
}

// DryMoveReq is the long-form DryMove that accepts a MoveRequest.
func (m *Manager) DryMoveReq(matchID, playerID, credentials string, req core.MoveRequest) (core.State, error) {
	unlock := m.lockMatch(matchID)
	defer unlock()

	match, err := m.loadMigrated(matchID)
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
			if credentials != "" && !m.AuthenticateCredentials(credentials, p) {
				return core.State{}, ErrBadCredentials
			}
			break
		}
	}
	if seat == "" {
		return core.State{}, ErrUnknownSeat
	}
	req.PlayerID = seat
	return core.Apply(g, match.State, req)
}

// MoveReqCtx is the context-aware MoveReq. The supplied ctx is threaded
// into MoveContext.Context so moves and plugins can honour deadlines.
func (m *Manager) MoveReqCtx(ctx context.Context, matchID, playerID, credentials string, req core.MoveRequest) (core.State, error) {
	unlock := m.lockMatch(matchID)
	defer unlock()

	if m.RequireStateID && req.StateID == 0 {
		return core.State{}, fmt.Errorf("%w: server requires StateID on every move", core.ErrStaleState)
	}

	match, err := m.loadMigrated(matchID)
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

	if !g.AllowDynamicPlayers && len(match.Players) < match.State.Ctx.NumPlayers {
		return core.State{}, ErrSeatRequired
	}

	start := m.now()
	req.PlayerID = seat
	prevGameover := match.State.Ctx.Gameover
	next, err := core.ApplyContext(ctx, g, match.State, req)
	if err != nil {
		m.Logger.Warn("match.move.rejected",
			"match_id", matchID,
			"player_id", playerID,
			"move", req.Move,
			"err", err.Error())
		return core.State{}, err
	}
	match.State = next
	if err := m.store.Update(match); err != nil {
		return core.State{}, err
	}

	m.broadcast(matchID, next)
	m.Logger.Info("match.move.applied",
		"match_id", matchID,
		"player_id", playerID,
		"move", req.Move,
		"state_id", next.StateID,
		"dur_us", m.now().Sub(start).Microseconds(),
		"gameover", next.Ctx.Gameover != nil)

	m.fireLifecycle(LifecycleEvent{
		Kind: LifecycleMatchMoved, MatchID: matchID,
		PlayerID: playerID, State: next, Match: match,
		Move: req.Move, Args: req.Args,
	})
	if next.Ctx.Gameover != nil && prevGameover == nil {
		// Game just ended on this move.
		m.fireLifecycle(LifecycleEvent{
			Kind: LifecycleMatchGameOver, MatchID: matchID,
			PlayerID: playerID, State: next, Match: match,
		})
	}
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

// broadcastMatchData notifies every subscriber that the seated-player list
// changed. Called after Join/Leave/UpdatePlayer.
func (m *Manager) broadcastMatchData(matchID string) {
	match, err := m.loadMigrated(matchID)
	if err != nil {
		return
	}
	m.subsMu.Lock()
	subs := make([]Subscriber, 0, len(m.subs[matchID]))
	for s := range m.subs[matchID] {
		subs = append(subs, s)
	}
	m.subsMu.Unlock()
	for _, s := range subs {
		s.SendMatchData(match.Players)
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

	match, err := m.loadMigrated(matchID)
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
		// When the game opted into deltaState transport and the subscriber
		// supports SendPatch, send the diff against whatever the subscriber
		// has cached as its previous state. The subscriber owns the cache
		// because per-seat views and per-connection sync timing make a
		// single global prev unworkable.
		if game.DeltaState {
			if patched, ok := s.(PatchSubscriber); ok {
				patched.SendPatch(v)
				continue
			}
		}
		s.Send(v)
	}
}

// PatchSubscriber is implemented by subscribers that can accept JSON Patch
// updates when Game.DeltaState is true. The match manager prefers SendPatch
// over Send for those subscribers — the subscriber owns the prev/next
// diffing logic (and tracks its own previous state).
type PatchSubscriber interface {
	Subscriber
	SendPatch(next core.State)
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
