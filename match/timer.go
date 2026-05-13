package match

import (
	"sync"
	"time"

	"github.com/tjcran/boardgame-go/core"
)

// turnTimers holds one *time.Timer per active match. The manager schedules
// fresh timers after every state change that begins a new turn.
type turnTimers struct {
	mu     sync.Mutex
	timers map[string]*time.Timer
}

func newTurnTimers() *turnTimers { return &turnTimers{timers: map[string]*time.Timer{}} }

// scheduleTurnTimer (re)installs a deadline for matchID based on the
// active Turn.TimeBudget. Called after Create, Move, Reset, Import. If
// the active TurnConfig has no TimeBudget — or the game is over — any
// existing timer is cancelled.
func (m *Manager) scheduleTurnTimer(matchID string, state core.State, game *core.Game) {
	if m.timers == nil {
		return
	}
	turn := game.ScopeTurn(state.Ctx.Phase)
	budget := time.Duration(0)
	if turn != nil {
		budget = turn.TimeBudget
	}

	m.timers.mu.Lock()
	defer m.timers.mu.Unlock()
	if t, ok := m.timers.timers[matchID]; ok {
		t.Stop()
		delete(m.timers.timers, matchID)
	}
	if budget <= 0 || state.Ctx.Gameover != nil {
		return
	}
	turnAtSchedule := state.Ctx.Turn
	m.timers.timers[matchID] = time.AfterFunc(budget, func() {
		_ = m.AutoExpire(matchID, turnAtSchedule)
	})
}

// cancelTurnTimer drops the timer for matchID if any. Called from Wipe-like
// paths and from the janitor.
func (m *Manager) cancelTurnTimer(matchID string) {
	if m.timers == nil {
		return
	}
	m.timers.mu.Lock()
	defer m.timers.mu.Unlock()
	if t, ok := m.timers.timers[matchID]; ok {
		t.Stop()
		delete(m.timers.timers, matchID)
	}
}

// AutoExpire force-ends the current turn IF the turn at fire time still
// matches expectedTurn (a stale firing — the turn advanced via a real
// move before the timer ran — is a no-op). Used by the timer scheduler;
// can also be called from external clocks / cron.
//
// The deferred state update broadcasts to subscribers like any other
// move, and emits a LifecycleMatchMoved event so app code sees the
// expiry as a normal transition.
func (m *Manager) AutoExpire(matchID string, expectedTurn int) error {
	unlock := m.lockMatch(matchID)
	defer unlock()

	match, err := m.loadMigrated(matchID)
	if err != nil {
		return err
	}
	if match.State.Ctx.Turn != expectedTurn {
		// Stale timer fire — a real move advanced the turn first.
		return nil
	}
	if match.State.Ctx.Gameover != nil {
		return nil
	}
	g := m.Game(match.GameName)
	if g == nil {
		return nil
	}
	next := core.ForceEndTurn(g, match.State)
	match.State = next
	if err := m.store.Update(match); err != nil {
		return err
	}
	m.broadcast(matchID, next)
	m.Logger.Info("match.turn_expired",
		"match_id", matchID, "turn", expectedTurn, "state_id", next.StateID)
	m.fireLifecycle(LifecycleEvent{
		Kind: LifecycleMatchMoved, MatchID: matchID,
		State: next, Match: match,
		Move: "__turn_expired__",
	})
	// Schedule the next turn's timer.
	m.scheduleTurnTimer(matchID, next, g)
	return nil
}
