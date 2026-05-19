package core

import (
	"context"
	"fmt"
	"time"
)

// MoveContext is the single argument passed to every move function and game
// hook. It mirrors boardgame.io's destructured `{ G, ctx, events, random,
// playerID, ...plugins }` object.
//
// Moves are deterministic: given the same MoveContext and args, a MoveFn must
// produce the same result. Don't capture wall-clock time or use Go's `rand`
// package — use mc.Random.
type MoveContext struct {
	G        G
	Ctx      Ctx
	PlayerID string  // who is making the move
	Events   *Events // queue side-effecting state transitions
	Random   *Random // seeded PRNG; will be nil until the Random plugin is wired

	// Context is the request-scoped context.Context propagated by the
	// transport (or context.Background() when Apply was called without
	// one). Moves with expensive computation can honour Context.Deadline
	// or Context.Done — boardgame.io has nothing equivalent. Go's
	// standard idiom is to thread context.Context through every blocking
	// call; we do the same.
	Context context.Context

	// Plugins holds plugin-supplied APIs keyed by Plugin.Name. Use
	// mc.Plugin("name") to fetch with a type assertion.
	Plugins map[string]any

	// Queue is the server-driven cascade scheduler. Use mc.Queue.Push
	// to schedule follow-up moves; mc.Queue.Block to pause for player
	// input. Drained by the reducer after the move + Events drain.
	Queue *Queue

	// ResumingBlock is the BlockSpec this move resumed, when applicable.
	// The reducer sets it on the MoveContext of an external move whose
	// MoveRequest.ResumeTag matched a pending block; the block has
	// already been removed from State.Blocks by the time the move runs.
	// Nil for non-resume external moves and for all drain-step moves.
	//
	// Use ResumingBlock.Target (when non-nil) plus ValidateSelection to
	// process the player's chosen targets in the resume handler.
	ResumingBlock *BlockSpec

	// extra holds AddLog entries appended during this MoveContext's
	// lifetime. Engine-only; not visible to plugins.
	extra *extraLog

	// chainedMoves counts Events.RunMove invocations within this
	// MoveContext, capped by maxChainedMoves in the reducer.
	chainedMoves int
}

// Plugin returns the API object exposed by the named plugin, or nil if no
// such plugin is loaded.
func (mc *MoveContext) Plugin(name string) any {
	if mc.Plugins == nil {
		return nil
	}
	return mc.Plugins[name]
}

// extraLog is a side channel for log entries appended by moves/hooks via
// AddLog. The reducer drains it into State.Log after the move's main path.
// Kept off the public surface so callers can't tamper.
//
// AddLog from a hook (BGIO issue #1228, which BGIO can't do because its
// log plugin only sees move-time updates) lands here too.
type extraLog struct {
	entries []LogEntry
}

// AddLog appends a custom LogEntry to the match log. Use from inside a
// move or hook to record a domain-specific event (e.g. "alice draws
// 3 cards") that should show up in the move log alongside automatic
// move entries.
//
// Empty Kind is rewritten to "custom".
func (mc *MoveContext) AddLog(entry LogEntry) {
	if mc.extra == nil {
		mc.extra = &extraLog{}
	}
	if entry.Kind == "" {
		entry.Kind = "custom"
	}
	if entry.Turn == 0 {
		entry.Turn = mc.Ctx.Turn
	}
	if entry.Phase == "" {
		entry.Phase = mc.Ctx.Phase
	}
	mc.extra.entries = append(mc.extra.entries, entry)
}

// MoveFn is the canonical move signature: take a MoveContext and zero or more
// args, return the new G or an error. Return core.ErrInvalidMove to reject
// the move; any other error is also surfaced as a 409 on the transport.
type MoveFn func(mc *MoveContext, args ...any) (G, error)

// HookFn is the signature for lifecycle hooks (onBegin/onEnd/onMove on a
// turn or phase, plus Game.OnEnd). Hooks can produce new G and can also
// queue events via mc.Events. They cannot fail — return the (possibly
// unchanged) G.
type HookFn func(mc *MoveContext) G

// Move is the long-form move definition. Use this when you need any of the
// per-move flags. For short-form moves, supply a bare MoveFn in the Moves
// map and the engine wraps it in `Move{Move: fn}`.
type Move struct {
	// Move is the move function. Required.
	Move MoveFn

	// Undoable controls whether this move can be undone within the current
	// turn. Accepts:
	//   - nil       → undoable (default)
	//   - bool      → fixed answer
	//   - UndoableFn → decided per call
	// Mirrors BGIO's per-move `undoable: bool | function`.
	Undoable any

	// Redact controls whether the move's args are hidden in the log for
	// other players (mirrors BGIO's `redact`). Accepts the same shapes as
	// Undoable.
	Redact any

	// ServerOnly is the inverse of BGIO's `client: false`: when true, the
	// move runs only on the server. Default (zero value) is false: the move
	// runs on both client and server.
	ServerOnly bool

	// NoLimit exempts this move from Turn.MinMoves / Turn.MaxMoves counting.
	NoLimit bool

	// IgnoreStaleStateID accepts the move even when the client's state ID is
	// out of date. Use with care — the move must defend itself.
	IgnoreStaleStateID bool

	// Timeout, if > 0, derives a context.WithTimeout from the caller's
	// MoveContext.Context for the duration of this move. The MoveFn
	// observes the deadline via mc.Context.Done(); a well-behaved move
	// returns early when the context is cancelled. Note: Go can't
	// forcibly stop a goroutine — Timeout is a cooperative cancellation
	// signal, not a guarantee.
	Timeout time.Duration

	// IgnoreBlocks lets this move bypass the ErrBlocked gate the
	// reducer applies when State.Blocks is non-empty. Use sparingly —
	// concede / forfeit / emergency-exit moves are the obvious case.
	IgnoreBlocks bool
}

// IsUndoable resolves the Undoable field for a given context. The default
// (nil) is true, matching BGIO's default behaviour.
func (m Move) IsUndoable(mc *MoveContext) bool {
	switch v := m.Undoable.(type) {
	case nil:
		return true
	case bool:
		return v
	case func(*MoveContext) bool:
		return v(mc)
	case UndoableFn:
		return v(mc)
	default:
		return true
	}
}

// IsRedacted resolves the Redact field for a given context. Default is
// false (no redaction).
func (m Move) IsRedacted(mc *MoveContext) bool {
	switch v := m.Redact.(type) {
	case nil:
		return false
	case bool:
		return v
	case func(*MoveContext) bool:
		return v(mc)
	case RedactFn:
		return v(mc)
	default:
		return false
	}
}

// UndoableFn lets games decide per-call whether a move can be undone.
type UndoableFn func(mc *MoveContext) bool

// RedactFn lets games decide per-call whether a move's args should be hidden
// in the log to other players.
type RedactFn func(mc *MoveContext) bool

// asMove normalises a Moves map entry. The map's value can be either a
// MoveFn (or func with the same shape) for short-form, or a Move struct
// for long-form. Anything else is a config error.
func asMove(v any) (Move, error) {
	switch m := v.(type) {
	case Move:
		if m.Move == nil {
			return Move{}, fmt.Errorf("move has nil Move function")
		}
		return m, nil
	case MoveFn:
		return Move{Move: m}, nil
	case func(*MoveContext, ...any) (G, error):
		return Move{Move: m}, nil
	default:
		return Move{}, fmt.Errorf("unsupported move type %T (expected MoveFn or Move)", v)
	}
}
