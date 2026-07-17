package ccg

import (
	"errors"

	"github.com/tjcran/boardgame-go/core"
)

// Priority pass-loop: the protocol layer for response windows.
//
// Stack games gate progress on consent — after an action, every
// player in some order gets the chance to respond before anything
// resolves. The protocol is always the same shape: a window opens
// over an ordered set of players; the holder either acts (resetting
// the consent count) or passes (rotating to the next player); when
// every player passes in unbroken succession, the window closes and
// the game advances (typically: resolve the top pending effect and
// open a fresh window).
//
// PriorityState is that protocol and nothing else. What it
// deliberately does NOT know: how the order is computed (an
// active-player-first rotation of ctx.PlayOrder is the classic
// choice — the game supplies the slice), what moves the holder may
// make (put them in the window stage's move table), what "everyone
// passed" means for the game (resolve an effect, advance a step, end
// the turn), or when windows open at all.
//
// Engine wiring is via events.SetActivePlayers: each protocol call
// queues a single-player Value config gating the game to the current
// holder in the window's stage, so out-of-window moves are rejected
// by the engine's normal authorization and the stage's move table
// scopes what responses are legal. Pass "" as the stage to run the
// protocol without engine gating (the game enforces its own move
// legality) — note "" here means NO SetActivePlayers calls, not
// core.StageNull; games that want StageNull gating call
// SetActivePlayers themselves.
//
// The canonical loop, in the game's response/pass moves:
//
//	// pass move:
//	closed, err := g.Priority.Pass(mc)
//	if closed {
//	    eff, ok, err := g.ResolveNext(ccg.HaltWhileOpen(&g.Priority, ccg.PickBack), table)
//	    // …flush any staged triggers, then reopen:
//	    g.Priority.OpenWindow(mc, apnapOrder, "respond")
//	}
//
//	// cast/response move, after paying costs and pushing the effect:
//	g.Priority.ActionTaken(mc, true) // true = actor retains priority
//
// One engine turn, many windows: turn and phase boundaries wipe
// ctx.ActivePlayers but NOT this struct (it lives in the game's G),
// so close every window before ending the turn; call Reset from a
// turn OnBegin hook to clear a stale window after a forced boundary.
type PriorityState struct {
	// Open reports whether a window is in progress.
	Open bool `json:"open,omitempty"`
	// Order is the fixed player rotation for this window, set at
	// OpenWindow.
	Order []string `json:"order,omitempty"`
	// HolderIdx indexes Order at the player currently holding
	// priority.
	HolderIdx int `json:"holder_idx,omitempty"`
	// Passes counts consecutive passes since the last action. The
	// window closes when it reaches len(Order).
	Passes int `json:"passes,omitempty"`
	// Stage is the stage name each SetActivePlayers gates the holder
	// into. Empty disables engine wiring entirely.
	Stage string `json:"stage,omitempty"`
}

// ErrPriorityOpen is returned by OpenWindow when a window is already
// in progress — close or Reset it first; nesting is not part of the
// protocol (games that want nested windows keep a slice of
// PriorityStates).
var ErrPriorityOpen = errors.New("ccg: priority window already open")

// ErrPriorityClosed is returned by Pass / ActionTaken / Drop when no
// window is open.
var ErrPriorityClosed = errors.New("ccg: no priority window open")

// ErrNotPriorityHolder is returned by Pass / ActionTaken when the
// mover isn't the player currently holding priority.
var ErrNotPriorityHolder = errors.New("ccg: player does not hold priority")

// ErrBadPriorityOrder is returned by OpenWindow for an empty order or
// one containing duplicates or empty player IDs — all three corrupt
// the all-passed threshold.
var ErrBadPriorityOrder = errors.New("ccg: invalid priority order")

// ErrNotInPriorityOrder is returned by Drop for a player who isn't
// part of the open window.
var ErrNotInPriorityOrder = errors.New("ccg: player not in priority order")

// IsOpen reports whether a window is in progress.
func (p *PriorityState) IsOpen() bool { return p.Open }

// Holder returns the player currently holding priority, or "" when
// no window is open.
func (p *PriorityState) Holder() string {
	if !p.Open || p.HolderIdx < 0 || p.HolderIdx >= len(p.Order) {
		return ""
	}
	return p.Order[p.HolderIdx]
}

// OpenWindow starts a window over players, in slice order — the game
// computes the rotation (players[0] receives priority first). With a
// non-empty stage, queues SetActivePlayers gating the game to
// players[0] in that stage. The order is defensively copied.
func (p *PriorityState) OpenWindow(mc *core.MoveContext, players []string, stage string) error {
	if p.Open {
		return ErrPriorityOpen
	}
	if len(players) == 0 {
		return ErrBadPriorityOrder
	}
	seen := make(map[string]bool, len(players))
	for _, pid := range players {
		if pid == "" || seen[pid] {
			return ErrBadPriorityOrder
		}
		seen[pid] = true
	}
	p.Open = true
	p.Order = append([]string(nil), players...)
	p.HolderIdx = 0
	p.Passes = 0
	p.Stage = stage
	p.gateHolder(mc)
	return nil
}

// Pass records a pass by mc.PlayerID. When every player in Order has
// passed in unbroken succession the window closes — the active set is
// restored to the engine default and closed=true is returned; the
// caller then advances the game (resolve the top effect, advance a
// step, …) and typically reopens. Otherwise priority rotates to the
// next player in Order.
func (p *PriorityState) Pass(mc *core.MoveContext) (closed bool, err error) {
	if !p.Open {
		return false, ErrPriorityClosed
	}
	if mc.PlayerID != p.Holder() {
		return false, ErrNotPriorityHolder
	}
	p.Passes++
	if p.Passes >= len(p.Order) {
		p.close(mc)
		return true, nil
	}
	p.HolderIdx = (p.HolderIdx + 1) % len(p.Order)
	p.gateHolder(mc)
	return false, nil
}

// ActionTaken records that mc.PlayerID acted, resetting the
// consecutive-pass count. retain=true keeps priority with the actor
// (the usual stack-game rule: you may respond to your own action);
// retain=false rotates to the next player in Order.
func (p *PriorityState) ActionTaken(mc *core.MoveContext, retain bool) error {
	if !p.Open {
		return ErrPriorityClosed
	}
	if mc.PlayerID != p.Holder() {
		return ErrNotPriorityHolder
	}
	p.Passes = 0
	if !retain {
		p.HolderIdx = (p.HolderIdx + 1) % len(p.Order)
		p.gateHolder(mc)
	}
	return nil
}

// Drop removes a player from the window mid-flight (concede,
// RemovePlayer, loss while responses are pending). If they held
// priority it rotates to the next player. The all-passed threshold
// shrinks with the order, so a window can close as a consequence:
// closed=true means the remaining players' passes now suffice and
// the caller should advance as if Pass had closed it. Dropping the
// last player always closes.
func (p *PriorityState) Drop(mc *core.MoveContext, player string) (closed bool, err error) {
	if !p.Open {
		return false, ErrPriorityClosed
	}
	idx := -1
	for i, pid := range p.Order {
		if pid == player {
			idx = i
			break
		}
	}
	if idx < 0 {
		return false, ErrNotInPriorityOrder
	}
	held := idx == p.HolderIdx
	p.Order = append(p.Order[:idx], p.Order[idx+1:]...)
	if len(p.Order) == 0 || p.Passes >= len(p.Order) {
		p.close(mc)
		return true, nil
	}
	if idx < p.HolderIdx {
		p.HolderIdx--
	}
	p.HolderIdx %= len(p.Order)
	if held {
		p.gateHolder(mc)
	}
	return false, nil
}

// CloseWindow force-closes an open window, restoring the engine's
// default active set. No-op when already closed. Use before ending
// the turn; for cleanup AFTER a forced turn/phase boundary (where the
// engine already wiped ctx.ActivePlayers) use Reset instead.
func (p *PriorityState) CloseWindow(mc *core.MoveContext) {
	if !p.Open {
		return
	}
	p.close(mc)
}

// Reset clears the protocol state without touching the engine — for
// turn OnBegin hygiene after a boundary wipe, where issuing
// SetActivePlayers would stomp the new turn's own config.
func (p *PriorityState) Reset() {
	*p = PriorityState{}
}

// HaltWhileOpen wraps a Picker so ResolveNext halts (-1) while the
// window is open — pending effects wait for consent, whatever order
// the inner picker implements.
func HaltWhileOpen(p *PriorityState, inner Picker) Picker {
	return func(pending []Effect) int {
		if p.Open {
			return -1
		}
		return inner(pending)
	}
}

// gateHolder queues the SetActivePlayers event for the current
// holder. No-op without engine wiring (empty Stage).
func (p *PriorityState) gateHolder(mc *core.MoveContext) {
	if p.Stage == "" {
		return
	}
	mc.Events.SetActivePlayers(core.ActivePlayersConfig{
		Value: map[string]string{p.Holder(): p.Stage},
	})
}

// close resets protocol state and, with engine wiring, restores the
// default active set (an empty config installs an empty map, which
// the engine normalises to nil — only the current player moves).
func (p *PriorityState) close(mc *core.MoveContext) {
	wired := p.Stage != ""
	*p = PriorityState{}
	if wired {
		mc.Events.SetActivePlayers(core.ActivePlayersConfig{})
	}
}
