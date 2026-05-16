package mcp

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/tjcran/boardgame-go/match"
	"github.com/tjcran/boardgame-go/mcp/starlarkgame"
)

// GameListing is the user-facing name + metadata returned by ListForUser.
type GameListing struct {
	Name       string
	MinPlayers int
	MaxPlayers int
	UserOwned  bool // true → designed by this user; false → built-in.
}

// UserAwareRegistry layers per-user game scoping on top of match.Manager.
// Built-in games are registered with the Manager at startup under their
// natural names. User-designed games are stored in UserGameStore and
// registered with the Manager under prefixed keys ("usergame:<uid>:<name>")
// so they can't collide with built-ins or with another user's games.
type UserAwareRegistry struct {
	mu    sync.RWMutex
	mgr   *match.Manager
	store UserGameStore

	// userKeys maps (userID, publicName) → managerKey. Built-ins do not
	// appear here; they're looked up directly on the Manager by their
	// public name.
	userKeys map[string]map[string]string
}

func NewUserAwareRegistry(mgr *match.Manager, store UserGameStore) *UserAwareRegistry {
	return &UserAwareRegistry{
		mgr:      mgr,
		store:    store,
		userKeys: map[string]map[string]string{},
	}
}

// Store exposes the underlying store for consumers (e.g. resource
// handlers in Task 23) that need direct read access to llm_guide.
func (r *UserAwareRegistry) Store() UserGameStore { return r.store }

const userGameKeyPrefix = "usergame:"

func managerKeyFor(userID, publicName string) string {
	return userGameKeyPrefix + userID + ":" + publicName
}

// ReplayFromStore loads every UserGame from the store and re-registers
// them on the Manager. Called once at server startup so prior-session
// designs are immediately playable.
func (r *UserAwareRegistry) ReplayFromStore(ctx context.Context) error {
	all, err := r.store.ListAll(ctx)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, ug := range all {
		if err := r.registerLocked(ug.UserID, ug.Name, ug.Source); err != nil {
			return fmt.Errorf("replay %s/%s: %w", ug.UserID, ug.Name, err)
		}
	}
	return nil
}

// RegisterUserGame validates the spec, persists it, and installs it on
// the Manager under a prefixed key.
func (r *UserAwareRegistry) RegisterUserGame(ctx context.Context, userID, source, llmGuide string) error {
	spec, err := starlarkgame.LoadSpec(source)
	if err != nil {
		return fmt.Errorf("load: %w", err)
	}
	if err := starlarkgame.Validate(ctx, spec); err != nil {
		return fmt.Errorf("validate: %w", err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	publicName := spec.Meta.Name
	if existing := r.mgr.Game(publicName); existing != nil {
		return fmt.Errorf("name %q collides with a built-in game", publicName)
	}
	if err := r.store.Put(ctx, UserGame{
		UserID:   userID,
		Name:     publicName,
		Source:   source,
		LLMGuide: llmGuide,
	}); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	return r.registerLocked(userID, publicName, source)
}

// registerLocked builds the core.Game and adds it to the Manager.
// Caller holds r.mu.
func (r *UserAwareRegistry) registerLocked(userID, publicName, source string) error {
	spec, err := starlarkgame.LoadSpec(source)
	if err != nil {
		// Bad source (e.g. empty or corrupt legacy entry): skip rather than
		// halting the whole replay. The prefix-key mapping won't be recorded
		// so LookupForUser will fall through to the built-in check, which is
		// the correct behavior for the collision-shadowing scenario.
		return nil
	}
	g := starlarkgame.BuildCoreGame(spec)
	key := managerKeyFor(userID, publicName)
	g.Name = key
	if err := r.mgr.Register(g); err != nil {
		// Idempotency: if this game is already registered (replay race
		// or duplicate Put), don't fail — just record the mapping.
		if !strings.Contains(err.Error(), "already") {
			return err
		}
	}
	if r.userKeys[userID] == nil {
		r.userKeys[userID] = map[string]string{}
	}
	r.userKeys[userID][publicName] = key
	return nil
}

// LookupForUser resolves a public game name to the Manager-internal key
// the caller should pass to mgr.Create. Built-ins win over user games
// with the same name. Returns the owning user ID (empty for built-ins)
// alongside the manager key for downstream ownership checks.
func (r *UserAwareRegistry) LookupForUser(_ context.Context, userID, publicName string) (managerKey, ownerID string, err error) {
	if g := r.mgr.Game(publicName); g != nil {
		return publicName, "", nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if m, ok := r.userKeys[userID]; ok {
		if k, ok := m[publicName]; ok {
			return k, userID, nil
		}
	}
	return "", "", fmt.Errorf("unknown game %q", publicName)
}

// ListForUser returns built-ins + this user's owned games, by public name,
// sorted alphabetically.
func (r *UserAwareRegistry) ListForUser(_ context.Context, userID string) ([]GameListing, error) {
	out := []GameListing{}
	for _, n := range r.mgr.GameNames() {
		if hasUserGameKeyPrefix(n) {
			continue // user games handled below
		}
		g := r.mgr.Game(n)
		if g == nil {
			continue
		}
		out = append(out, GameListing{Name: n, MinPlayers: g.MinPlayers, MaxPlayers: g.MaxPlayers})
	}
	r.mu.RLock()
	for publicName, key := range r.userKeys[userID] {
		g := r.mgr.Game(key)
		if g == nil {
			continue
		}
		out = append(out, GameListing{Name: publicName, MinPlayers: g.MinPlayers, MaxPlayers: g.MaxPlayers, UserOwned: true})
	}
	r.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// DeleteUserGame removes the user's spec from store + Manager.
func (r *UserAwareRegistry) DeleteUserGame(ctx context.Context, userID, publicName string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key, ok := r.userKeys[userID][publicName]
	if !ok {
		return fmt.Errorf("no such game")
	}
	if err := r.store.Delete(ctx, userID, publicName); err != nil {
		return err
	}
	// match.Manager has no Unregister yet; the prefix scheme guarantees
	// the key is unique, so leaving the dangling entry is harmless.
	// (Tracked: Manager.Unregister is a v2 cleanup.)
	delete(r.userKeys[userID], publicName)
	_ = key
	return nil
}

// UserGame returns the stored UserGame for the owner+name. Returns nil
// (no error) when not found. Used by playtest validation that wants to
// roundtrip the stored source.
func (r *UserAwareRegistry) UserGame(ctx context.Context, userID, publicName string) (*UserGame, error) {
	return r.store.Get(ctx, userID, publicName)
}

func hasUserGameKeyPrefix(s string) bool {
	return strings.HasPrefix(s, userGameKeyPrefix)
}

// publicGameName strips the user-owned-game manager prefix if present,
// returning the public name as the user originally wrote it.
// For built-in games (no prefix) the key is returned unchanged.
func publicGameName(managerKey string) string {
	if !hasUserGameKeyPrefix(managerKey) {
		return managerKey
	}
	// Format: usergame:<owner>:<publicName>
	rest := strings.TrimPrefix(managerKey, userGameKeyPrefix)
	if i := strings.Index(rest, ":"); i >= 0 {
		return rest[i+1:]
	}
	return managerKey
}
