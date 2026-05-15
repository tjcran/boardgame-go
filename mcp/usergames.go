package mcp

import (
	"context"
	"sort"
	"sync"
	"time"
)

// UserGame is one row in the user_games table. Source is the Starlark
// spec source; LLMGuide is the optional rules-and-strategy markdown
// surfaced via the game://owner/name/guide MCP resource.
type UserGame struct {
	UserID    string
	Name      string
	Source    string
	LLMGuide  string
	CreatedAt time.Time
}

// UserGameStore is the storage abstraction over designed-game specs.
// Implementations: in-memory (stdio mode) and Postgres (hosted mode).
type UserGameStore interface {
	Put(ctx context.Context, ug UserGame) error
	Get(ctx context.Context, userID, name string) (*UserGame, error)
	List(ctx context.Context, userID string) ([]string, error)
	ListAll(ctx context.Context) ([]UserGame, error) // used at startup to replay into Manager.
	Delete(ctx context.Context, userID, name string) error
}

// NewInMemoryUserGames returns a goroutine-safe in-memory implementation
// suitable for stdio mode and unit tests.
func NewInMemoryUserGames() *InMemoryUserGames {
	return &InMemoryUserGames{m: map[string]UserGame{}}
}

type InMemoryUserGames struct {
	mu sync.RWMutex
	m  map[string]UserGame // key: userID + "\x00" + name
}

func ugKey(userID, name string) string { return userID + "\x00" + name }

func (s *InMemoryUserGames) Put(_ context.Context, ug UserGame) error {
	if ug.CreatedAt.IsZero() {
		ug.CreatedAt = time.Now().UTC()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[ugKey(ug.UserID, ug.Name)] = ug
	return nil
}

func (s *InMemoryUserGames) Get(_ context.Context, userID, name string) (*UserGame, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if ug, ok := s.m[ugKey(userID, name)]; ok {
		ugCopy := ug
		return &ugCopy, nil
	}
	return nil, nil
}

func (s *InMemoryUserGames) List(_ context.Context, userID string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []string{}
	prefix := userID + "\x00"
	for k := range s.m {
		if len(k) > len(prefix) && k[:len(prefix)] == prefix {
			out = append(out, k[len(prefix):])
		}
	}
	sort.Strings(out)
	return out, nil
}

func (s *InMemoryUserGames) ListAll(_ context.Context) ([]UserGame, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]UserGame, 0, len(s.m))
	for _, v := range s.m {
		out = append(out, v)
	}
	return out, nil
}

func (s *InMemoryUserGames) Delete(_ context.Context, userID, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, ugKey(userID, name))
	return nil
}
