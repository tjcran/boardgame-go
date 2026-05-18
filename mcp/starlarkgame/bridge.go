package starlarkgame

import (
	"fmt"

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
	return starlarkstruct.FromStringDict(starlark.String("ctx"), attrs)
}

// bridgeRandom is defined in random.go (Task 5). It wraps core.Random and
// exposes ctx.random.range/shuffle/choice to Starlark.
