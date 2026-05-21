package starlarkgame

import (
	"fmt"

	"github.com/tjcran/boardgame-go/core"
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

	// rng is set by attachRandom (Task 5). Nil → ctx.random raises.
	rng *bridgeRandom
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

// bridgeRandom is defined in random.go (Task 5). It wraps core.Random and
// exposes ctx.random.range/shuffle/choice to Starlark.
