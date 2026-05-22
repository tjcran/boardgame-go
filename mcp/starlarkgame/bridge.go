package starlarkgame

import (
	"fmt"

	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/mcp/modulebridge"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

// BridgeCtx carries per-call data into Starlark via the `ctx` argument.
// The Starlark side sees it as a frozen struct with attribute access.
// Each game-engine call (Setup, Apply, EndIf, …) constructs one of these.
type BridgeCtx struct {
	PlayerID   string
	NumPlayers int
	Phase      string // current phase name, "" when game has no phases
	Log        []string

	// Events, when set, gives Starlark apply functions access to the
	// engine event queue via ctx.events.set_stage(name)/end_stage().
	// Nil for pure callbacks (end_if, legal_moves, player_view) — those
	// don't get an events object exposed at all.
	Events *core.Events

	// Modules holds live engine-module states for ctx.modules.* bindings.
	// Keyed by module name ("ccg"). Nil when the spec declares no MODULES.
	Modules map[string]any

	// ReadOnly marks a speculative callback (legal_moves, end_if,
	// player_view) where the module state is shared with the live game.
	// When set, ctx.modules exposes only read ops; mutating ops error.
	ReadOnly bool

	// Queue, when set, exposes ctx.request_target to pause a move for a
	// player target selection. Set only for move applies.
	Queue *core.Queue

	// ResumingBlock, when set, is the block this move resumed — exposes
	// ctx.resuming_target() and ctx.validate_selection().
	ResumingBlock *core.BlockSpec

	// rng is set by attachRandom (Task 5). Nil → ctx.random raises.
	rng *bridgeRandom
}

// NewReadCtx builds the ctx for a speculative read callback — end_if,
// legal_moves, player_view, phase end_if. Module ops are exposed
// read-only so the callback cannot mutate the shared live state.
//
// NewWriteCtx / NewReadCtx are the only sanctioned ways to build a
// callback ctx: mods is a required argument (a forgotten struct field
// is exactly how playtest_draft once ran with no modules) and ReadOnly
// is fixed by the choice of constructor, not left to each call site.
// Callers layer on the per-callback extras afterward (Phase, Events,
// Queue, ResumingBlock) and attach the seeded RNG where applicable.
func NewReadCtx(numPlayers int, playerID string, mods map[string]any) *BridgeCtx {
	return &BridgeCtx{NumPlayers: numPlayers, PlayerID: playerID, Modules: mods, ReadOnly: true}
}

// NewWriteCtx builds the ctx for a mutating callback — setup, move
// apply, hooks. Module ops may mutate the live state.
func NewWriteCtx(numPlayers int, playerID string, mods map[string]any) *BridgeCtx {
	return &BridgeCtx{NumPlayers: numPlayers, PlayerID: playerID, Modules: mods}
}

// asStarlark returns the `ctx` value Starlark code receives. It's a
// starlarkstruct whose attributes proxy to BridgeCtx fields, so updates
// (e.g. ctx.log) flow back to the Go side.
func (c *BridgeCtx) asStarlark() starlark.Value {
	attrs := starlark.StringDict{
		"player_id":   starlark.String(c.PlayerID),
		"num_players": starlark.MakeInt(c.NumPlayers),
		"phase":       starlark.String(c.Phase),
		"log": starlark.NewBuiltin("log",
			func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
				if len(args) != 1 {
					return nil, fmt.Errorf("log: expected 1 arg, got %d", len(args))
				}
				s, ok := args[0].(starlark.String)
				if !ok {
					return nil, fmt.Errorf("log: arg must be string, got %s", args[0].Type())
				}
				c.Log = append(c.Log, string(s))
				return starlark.None, nil
			}),
	}
	if c.rng != nil {
		attrs["random"] = c.rng.asStarlark()
	}
	if c.Events != nil {
		attrs["events"] = c.eventsAsStarlark()
	}
	if len(c.Modules) > 0 {
		attrs["modules"] = c.modulesAsStarlark()
	}
	if c.Queue != nil {
		attrs["request_target"] = c.requestTargetBuiltin()
	}
	if c.ResumingBlock != nil {
		attrs["resuming_target"] = c.resumingTargetBuiltin()
		attrs["validate_selection"] = c.validateSelectionBuiltin()
	}
	return starlarkstruct.FromStringDict(starlark.String("ctx"), attrs)
}

// eventsAsStarlark returns the `ctx.events` substruct exposed to apply
// functions. Only the move-scoped pieces are surfaced — set_stage and
// end_stage cover the loveletter-style "now pick a target" and
// "discard to N" patterns. SetActivePlayers, EndPhase, EndGame, etc.
// are deliberately out of scope until a concrete spec needs them.
func (c *BridgeCtx) eventsAsStarlark() starlark.Value {
	setStage := starlark.NewBuiltin("set_stage",
		func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("ctx.events.set_stage: expected 1 arg, got %d", len(args))
			}
			s, ok := args[0].(starlark.String)
			if !ok {
				return nil, fmt.Errorf("ctx.events.set_stage: arg must be string, got %s", args[0].Type())
			}
			c.Events.SetStage(string(s))
			return starlark.None, nil
		})
	endStage := starlark.NewBuiltin("end_stage",
		func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
			if len(args) != 0 {
				return nil, fmt.Errorf("ctx.events.end_stage: takes no args, got %d", len(args))
			}
			c.Events.EndStage()
			return starlark.None, nil
		})
	return starlarkstruct.FromStringDict(starlark.String("events"), starlark.StringDict{
		"set_stage": setStage,
		"end_stage": endStage,
	})
}

// modulesAsStarlark builds the ctx.modules struct: one attribute per
// live module, each a struct of op builtins bound to that module's
// live state. Args are passed as Starlark keyword arguments.
func (c *BridgeCtx) modulesAsStarlark() starlark.Value {
	var rng *core.Random
	if c.rng != nil {
		rng = c.rng.rng
	}
	modAttrs := starlark.StringDict{}
	for name := range c.Modules {
		reg := modulebridge.RegistryFor(name)
		if reg == nil {
			continue
		}
		opAttrs := starlark.StringDict{}
		for _, op := range reg.Ops(name) {
			op := op
			// In read-only contexts (legal_moves / end_if / player_view),
			// mutating ops are exposed as a stub that errors instead of
			// running — they would corrupt the speculative shared state.
			if c.ReadOnly && !op.ReadOnly {
				opAttrs[op.Name] = starlark.NewBuiltin(name+"."+op.Name,
					func(_ *starlark.Thread, _ *starlark.Builtin, _ starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
						return nil, fmt.Errorf("%s.%s mutates state and is not callable from a read-only context (legal_moves/end_if/player_view); only read ops are", name, op.Name)
					})
				continue
			}
			opAttrs[op.Name] = starlark.NewBuiltin(name+"."+op.Name,
				func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
					if len(args) != 0 {
						return nil, fmt.Errorf("%s.%s: use keyword args only", name, op.Name)
					}
					m := make(map[string]any, len(kwargs))
					for _, kv := range kwargs {
						k, ok := kv[0].(starlark.String)
						if !ok {
							return nil, fmt.Errorf("%s.%s: non-string kwarg", name, op.Name)
						}
						gv, err := ToGo(kv[1])
						if err != nil {
							return nil, err
						}
						m[string(k)] = gv
					}
					res, err := op.Call(c.Modules, m, rng)
					if err != nil {
						return nil, err
					}
					return ToStarlark(res)
				})
		}
		modAttrs[name] = starlarkstruct.FromStringDict(starlark.String(name), opAttrs)
	}
	return starlarkstruct.FromStringDict(starlark.String("modules"), modAttrs)
}

// requestTargetBuiltin returns ctx.request_target(...), which pauses the
// move cascade by recording a typed TargetRequest on the engine queue.
func (c *BridgeCtx) requestTargetBuiltin() starlark.Value {
	return starlark.NewBuiltin("request_target",
		func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
			if len(args) != 0 {
				return nil, fmt.Errorf("request_target: use keyword args only")
			}
			req := core.TargetRequest{}
			for _, kv := range kwargs {
				k, ok := kv[0].(starlark.String)
				if !ok {
					return nil, fmt.Errorf("request_target: non-string kwarg")
				}
				gv, err := ToGo(kv[1])
				if err != nil {
					return nil, err
				}
				switch string(k) {
				case "kind":
					s, ok := gv.(string)
					if !ok {
						return nil, fmt.Errorf("request_target: kind must be a string")
					}
					req.Kind = s
				case "candidates":
					lst, ok := gv.([]any)
					if !ok {
						return nil, fmt.Errorf("request_target: candidates must be a list")
					}
					req.Candidates = lst
				case "min":
					n, ok := gv.(int64)
					if !ok {
						return nil, fmt.Errorf("request_target: min must be an int")
					}
					req.Min = int(n)
				case "max":
					n, ok := gv.(int64)
					if !ok {
						return nil, fmt.Errorf("request_target: max must be an int")
					}
					req.Max = int(n)
				case "source":
					req.Source = gv
				case "data":
					if m, ok := gv.(map[string]any); ok {
						req.Data = m
					}
				default:
					return nil, fmt.Errorf("request_target: unknown arg %q", string(k))
				}
			}
			if req.Kind == "" {
				return nil, fmt.Errorf("request_target: kind is required")
			}
			c.Queue.RequestTarget(c.PlayerID, req)
			return starlark.None, nil
		})
}

// resumingTargetBuiltin returns ctx.resuming_target(), exposing the
// block this move resumed as a dict (or None when there is none).
func (c *BridgeCtx) resumingTargetBuiltin() starlark.Value {
	return starlark.NewBuiltin("resuming_target",
		func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
			if len(args) != 0 {
				return nil, fmt.Errorf("resuming_target: takes no args")
			}
			if c.ResumingBlock == nil || c.ResumingBlock.Target == nil {
				return starlark.None, nil
			}
			tr := c.ResumingBlock.Target
			out := map[string]any{
				"kind":       tr.Kind,
				"candidates": tr.Candidates,
				"min":        int64(tr.Min),
				"max":        int64(tr.Max),
			}
			if tr.Source != nil {
				out["source"] = tr.Source
			}
			if tr.Data != nil {
				out["data"] = map[string]any(tr.Data)
			}
			return ToStarlark(out)
		})
}

// validateSelectionBuiltin returns ctx.validate_selection(sel), which
// validates the player's pick against the resuming request and rejects
// the move (returns an error) when invalid.
func (c *BridgeCtx) validateSelectionBuiltin() starlark.Value {
	return starlark.NewBuiltin("validate_selection",
		func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("validate_selection: expected 1 arg (the selection list)")
			}
			if c.ResumingBlock == nil || c.ResumingBlock.Target == nil {
				return nil, fmt.Errorf("validate_selection: no resuming target request")
			}
			gv, err := ToGo(args[0])
			if err != nil {
				return nil, err
			}
			lst, ok := gv.([]any)
			if !ok {
				return nil, fmt.Errorf("validate_selection: arg must be a list")
			}
			if err := core.ValidateSelection(*c.ResumingBlock.Target, lst); err != nil {
				return nil, err
			}
			return starlark.None, nil
		})
}

// bridgeRandom is defined in random.go (Task 5). It wraps core.Random and
// exposes ctx.random.range/shuffle/choice to Starlark.
