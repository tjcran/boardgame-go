package core

import "fmt"

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

	// Plugins holds plugin-supplied APIs keyed by Plugin.Name. Use
	// mc.Plugin("name") to fetch with a type assertion.
	Plugins map[string]any
}

// Plugin returns the API object exposed by the named plugin, or nil if no
// such plugin is loaded.
func (mc *MoveContext) Plugin(name string) any {
	if mc.Plugins == nil {
		return nil
	}
	return mc.Plugins[name]
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
