package storage

import "sync"

// Memory is an in-process Storage. Fine for development and tests; lose
// power and you lose your matches. Concurrency-safe.
type Memory struct {
	mu      sync.RWMutex
	matches map[string]*Match
}

// NewMemory returns an empty in-memory store.
func NewMemory() *Memory {
	return &Memory{matches: map[string]*Match{}}
}

func (m *Memory) Create(match *Match) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.matches[match.ID] = cloneMatch(match)
	return nil
}

func (m *Memory) Get(id string) (*Match, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	mm, ok := m.matches[id]
	if !ok {
		return nil, ErrNotFound
	}
	return cloneMatch(mm), nil
}

func (m *Memory) Update(match *Match) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.matches[match.ID]; !ok {
		return ErrNotFound
	}
	m.matches[match.ID] = cloneMatch(match)
	return nil
}

func (m *Memory) List(gameName string) ([]*Match, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Match, 0, len(m.matches))
	for _, v := range m.matches {
		if gameName != "" && v.GameName != gameName {
			continue
		}
		out = append(out, cloneMatch(v))
	}
	return out, nil
}

// cloneMatch isolates callers from internal mutation. We share the State.G
// pointer because Apply produces fresh values; the slice of players is copied.
func cloneMatch(m *Match) *Match {
	cp := *m
	cp.Players = append([]Player(nil), m.Players...)
	return &cp
}
