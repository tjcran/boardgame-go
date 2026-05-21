package modulebridge

import (
	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/modules/ccg"
	"github.com/tjcran/boardgame-go/modules/economy"
)

func init() {
	stateFactories["economy"] = func() any { return &emptyState{} }
	registryByName["economy"] = EconomyRegistry()
}

var economyReg *Registry

func EconomyRegistry() *Registry {
	if economyReg == nil {
		economyReg = buildEconomyRegistry()
	}
	return economyReg
}

func poolFrom(modules map[string]any, args map[string]any) (economy.Pool, *ccg.State, error) {
	s, err := ccgFrom(modules)
	if err != nil {
		return economy.Pool{}, nil, err
	}
	ownerTok, err := argStr(args, "owner")
	if err != nil {
		return economy.Pool{}, nil, err
	}
	owner, err := ParseEntityToken(ownerTok)
	if err != nil {
		return economy.Pool{}, nil, err
	}
	kind, err := argStr(args, "kind")
	if err != nil {
		return economy.Pool{}, nil, err
	}
	capv := 0
	if _, ok := args["cap"]; ok {
		if c, err := argInt(args, "cap"); err == nil {
			capv = c
		}
	}
	return economy.Pool{Owner: owner, Kind: kind, Cap: capv}, s, nil
}

func buildEconomyRegistry() *Registry {
	r := NewRegistry()

	r.Add(Op{Module: "economy", Name: "current", MCPTool: "economy_current",
		Call: func(modules map[string]any, args map[string]any, rng *core.Random) (any, error) {
			p, s, err := poolFrom(modules, args)
			if err != nil {
				return nil, err
			}
			return int64(p.Current(s)), nil
		}})

	r.Add(Op{Module: "economy", Name: "gain", MCPTool: "economy_gain",
		Call: func(modules map[string]any, args map[string]any, rng *core.Random) (any, error) {
			p, s, err := poolFrom(modules, args)
			if err != nil {
				return nil, err
			}
			n, err := argInt(args, "n")
			if err != nil {
				return nil, err
			}
			return int64(p.Gain(s, n)), nil
		}})

	r.Add(Op{Module: "economy", Name: "spend", MCPTool: "economy_spend",
		Call: func(modules map[string]any, args map[string]any, rng *core.Random) (any, error) {
			p, s, err := poolFrom(modules, args)
			if err != nil {
				return nil, err
			}
			n, err := argInt(args, "n")
			if err != nil {
				return nil, err
			}
			return nil, p.Spend(s, n)
		}})

	r.Add(Op{Module: "economy", Name: "set", MCPTool: "economy_set",
		Call: func(modules map[string]any, args map[string]any, rng *core.Random) (any, error) {
			p, s, err := poolFrom(modules, args)
			if err != nil {
				return nil, err
			}
			n, err := argInt(args, "n")
			if err != nil {
				return nil, err
			}
			return int64(p.Set(s, n)), nil
		}})

	r.Add(Op{Module: "economy", Name: "scaled", MCPTool: "economy_scaled",
		Call: func(modules map[string]any, args map[string]any, rng *core.Random) (any, error) {
			turn, err := argInt(args, "turn")
			if err != nil {
				return nil, err
			}
			base, err := argInt(args, "base")
			if err != nil {
				return nil, err
			}
			per, err := argInt(args, "per")
			if err != nil {
				return nil, err
			}
			maxv := 0
			if _, ok := args["max"]; ok {
				if m, err := argInt(args, "max"); err == nil {
					maxv = m
				}
			}
			return int64(economy.Scaled(turn, base, per, maxv)), nil
		}})

	return r
}
