package mcp

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// userIDKey is the private context key used to carry the authenticated
// user identity through the call stack. The transport layer attaches it
// (stdio sets "local"; HTTP extracts the OAuth subject); tool handlers
// read it via UserIDFromContext.
type userIDKey struct{}

// WithUserID returns a copy of ctx carrying the given userID. Used by
// the auth middleware and by stdio mode's "local" user.
func WithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, userIDKey{}, userID)
}

// UserIDFromContext extracts the user identity from ctx, or "" if none
// has been attached. Tool handlers compare against the ownership store
// before touching any match-scoped resource.
func UserIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(userIDKey{}).(string); ok {
		return v
	}
	return ""
}

// ErrNotOwned is returned by Tools when the authenticated user doesn't
// own the match they're trying to act on. Surfaced to MCP clients as an
// isError tool result, which the LLM can read and react to.
var ErrNotOwned = errors.New("match is not owned by this user")

// OwnershipStore tracks which user created which match, enforcing the
// "each user only acts on their own matches" property in hosted mode.
//
// In stdio mode this is typically a no-op store (or simply unset on
// Tools) because there's only one user per process.
type OwnershipStore interface {
	// Claim records that userID owns matchID. Called once at
	// create_match time.
	Claim(ctx context.Context, userID, matchID string) error

	// Owns reports whether the given user is the owner of matchID.
	// Returns false (without error) if the match has no recorded owner.
	Owns(ctx context.Context, userID, matchID string) (bool, error)

	// MatchesFor returns every matchID owned by userID. Mostly useful
	// for a future list_matches tool — included now so we don't have
	// to widen the interface later.
	MatchesFor(ctx context.Context, userID string) ([]string, error)
}

// MemoryOwnership is an in-memory OwnershipStore. Suitable for stdio
// mode, tests, and HTTP-mode dev deployments that haven't wired up a
// real database. Loses state on process exit.
type MemoryOwnership struct {
	mu       sync.RWMutex
	owners   map[string]string   // matchID → userID
	byUserID map[string][]string // userID → []matchID
}

// NewMemoryOwnership builds an empty in-memory store.
func NewMemoryOwnership() *MemoryOwnership {
	return &MemoryOwnership{
		owners:   map[string]string{},
		byUserID: map[string][]string{},
	}
}

func (m *MemoryOwnership) Claim(_ context.Context, userID, matchID string) error {
	if userID == "" {
		return errors.New("userID required")
	}
	if matchID == "" {
		return errors.New("matchID required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, ok := m.owners[matchID]; ok && existing != userID {
		return fmt.Errorf("match %s already owned by another user", matchID)
	}
	m.owners[matchID] = userID
	// Avoid duplicate entries on idempotent re-claims.
	for _, m := range m.byUserID[userID] {
		if m == matchID {
			return nil
		}
	}
	m.byUserID[userID] = append(m.byUserID[userID], matchID)
	return nil
}

func (m *MemoryOwnership) Owns(_ context.Context, userID, matchID string) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	owner, ok := m.owners[matchID]
	if !ok {
		return false, nil
	}
	return owner == userID, nil
}

func (m *MemoryOwnership) MatchesFor(_ context.Context, userID string) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	src := m.byUserID[userID]
	out := make([]string, len(src))
	copy(out, src)
	return out, nil
}

// requireOwnership is the gate every match-scoped tool runs through. With
// no OwnershipStore configured (Tools.Ownership == nil), single-tenant
// mode is in effect and the gate is a no-op. With one configured, the
// userID must be on the context AND must match the matchID's owner.
func (t *Tools) requireOwnership(ctx context.Context, matchID string) error {
	if t.Ownership == nil {
		return nil
	}
	userID := UserIDFromContext(ctx)
	if userID == "" {
		return errors.New("not authenticated: no userID on request context")
	}
	owns, err := t.Ownership.Owns(ctx, userID, matchID)
	if err != nil {
		return fmt.Errorf("ownership check: %w", err)
	}
	if !owns {
		return fmt.Errorf("%w: match=%s user=%s", ErrNotOwned, matchID, userID)
	}
	return nil
}
