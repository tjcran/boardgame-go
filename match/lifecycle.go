package match

import (
	"sync"

	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/storage"
)

// LifecycleEventKind tags the engine-level event a LifecycleEvent describes.
type LifecycleEventKind string

const (
	LifecycleMatchCreated  LifecycleEventKind = "match.created"
	LifecycleMatchJoined   LifecycleEventKind = "match.joined"
	LifecycleMatchLeft     LifecycleEventKind = "match.left"
	LifecycleMatchMoved    LifecycleEventKind = "match.moved"
	LifecycleMatchGameOver LifecycleEventKind = "match.gameOver"
	LifecycleMatchReset    LifecycleEventKind = "match.reset"
)

// LifecycleEvent is dispatched to handlers registered with
// Manager.OnLifecycle. PlayerID is set only for events tied to a
// specific seat (joined/left/moved). State is the post-event state of
// the match.
type LifecycleEvent struct {
	Kind     LifecycleEventKind
	MatchID  string
	PlayerID string
	State    core.State
	Match    *storage.Match
	// Move and Args are set for LifecycleMatchMoved events.
	Move string
	Args []any
}

// LifecycleHandler receives lifecycle events. Handlers run synchronously
// inside the Manager's call stack — keep them fast. For heavy work,
// dispatch to your own goroutine. Order across handlers is
// registration-time.
type LifecycleHandler func(event LifecycleEvent)

// OnLifecycle registers handler for every kind of lifecycle event.
// Returns an unsubscribe function. Use OnLifecycleKind for kind-filtered
// subscriptions.
func (m *Manager) OnLifecycle(handler LifecycleHandler) func() {
	return m.OnLifecycleKind("", handler)
}

// OnLifecycleKind registers handler for a specific event kind. Pass an
// empty kind to receive every event.
func (m *Manager) OnLifecycleKind(kind LifecycleEventKind, handler LifecycleHandler) func() {
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()
	if m.lifecycleSubs == nil {
		m.lifecycleSubs = map[LifecycleEventKind][]LifecycleHandler{}
	}
	m.lifecycleSubs[kind] = append(m.lifecycleSubs[kind], handler)
	idx := len(m.lifecycleSubs[kind]) - 1
	return func() {
		m.lifecycleMu.Lock()
		defer m.lifecycleMu.Unlock()
		subs := m.lifecycleSubs[kind]
		if idx < len(subs) {
			subs[idx] = nil
			m.lifecycleSubs[kind] = subs
		}
	}
}

// fireLifecycle dispatches one event to all matching handlers. Called
// from the manager's event sites (Create, Join, Leave, Move, Reset).
func (m *Manager) fireLifecycle(ev LifecycleEvent) {
	m.lifecycleMu.Lock()
	exact := append([]LifecycleHandler(nil), m.lifecycleSubs[ev.Kind]...)
	all := append([]LifecycleHandler(nil), m.lifecycleSubs[""]...)
	m.lifecycleMu.Unlock()
	for _, h := range exact {
		if h != nil {
			h(ev)
		}
	}
	for _, h := range all {
		if h != nil {
			h(ev)
		}
	}
}

// lifecycleState is the manager-owned bookkeeping for lifecycle hooks.
// Embedded in Manager via composition keeps the field cluster small.
type lifecycleState struct {
	mu   sync.Mutex
	subs map[LifecycleEventKind][]LifecycleHandler
}
