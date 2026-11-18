package match

import (
	"sync"

	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/storage"
)

// LifecycleEventKind tags the engine-level event a LifecycleEvent describes.
type LifecycleEventKind string

const (
	LifecycleMatchCreated      LifecycleEventKind = "match.created"
	LifecycleMatchJoined       LifecycleEventKind = "match.joined"
	LifecycleMatchLeft         LifecycleEventKind = "match.left"
	LifecycleMatchMoved        LifecycleEventKind = "match.moved"
	LifecycleMatchMoveRejected LifecycleEventKind = "match.moveRejected"
	LifecycleMatchGameOver     LifecycleEventKind = "match.gameOver"
	LifecycleMatchReset        LifecycleEventKind = "match.reset"
)

// LifecycleEvent is dispatched to handlers registered with
// Manager.OnLifecycle. PlayerID is set only for events tied to a
// specific seat (joined/left/moved/moveRejected). State is the
// post-event state of the match and Match the match itself; both are set
// for every kind. For LifecycleMatchMoveRejected nothing changed, so
// State is the state the move was rejected against (its Ctx is the
// pre-move turn, phase and stage context). Handlers read the match from
// the event; they cannot re-read it from the Manager (see
// LifecycleHandler).
type LifecycleEvent struct {
	Kind     LifecycleEventKind
	MatchID  string
	PlayerID string
	State    core.State
	Match    *storage.Match
	// Move and Args are set for LifecycleMatchMoved and
	// LifecycleMatchMoveRejected events.
	Move string
	Args []any
	// PrevState is the pre-move snapshot captured just before
	// core.ApplyContext ran (or, for a turn-timer expiry, before the
	// forced turn end). Populated only for LifecycleMatchMoved; zero
	// value for all other event kinds (a rejected move's pre-move state
	// is State). Equal Setup + equal moves
	// → equal PrevState (replay-safe).
	PrevState core.State
	// LogDelta is the slice of log entries appended during this move
	// and its drain — equal to State.Log[len(PrevState.Log):]. Provided
	// for observer convenience so consumers don't have to compute the
	// slice math themselves. Nil for non-move event kinds.
	LogDelta []core.LogEntry
	// Err is the rejection reason for LifecycleMatchMoveRejected events.
	// Nil for all other event kinds. Observers can use errors.Is to
	// dispatch on specific rejection causes (stale state, unauthorized,
	// blocked, unknown move, etc.).
	Err error
	// StateID is the match's current state ID at the time of a
	// LifecycleMatchMoveRejected event — useful for observers
	// correlating rejections to specific match-state revisions.
	// Zero for all other event kinds (which expose State.StateID
	// instead).
	StateID int
}

// LifecycleHandler receives lifecycle events. Handlers run synchronously
// inside the Manager's call stack, while it holds the match's lock
// (PlayAgain fires the new match's LifecycleMatchCreated while holding
// the old match's lock). So a handler must not block — for heavy or
// I/O-bound work, copy what it needs from the event and hand off to
// your own goroutine — and must not call any Manager method that takes
// a match lock on that match (State, Snapshot, View, ExportMatch,
// Authenticate, Move/MoveReq/DispatchServer, Join, Leave, UpdatePlayer,
// SetConnected, Reset, PlayAgain, …): the lock is not reentrant, so the
// match would deadlock for good. Everything a handler needs is on the
// event. Order across handlers is registration-time.
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
