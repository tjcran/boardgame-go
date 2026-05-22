package modulebridge

import (
	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/modules/ccg"
	"github.com/tjcran/boardgame-go/modules/shop"
)

func init() {
	stateFactories["shop"] = func() any { return &emptyState{} }
	registryByName["shop"] = ShopRegistry()
	rehydrators["shop"] = func(m map[string]any) (any, error) { return jsonRehydrate("shop", m) }
}

var shopReg *Registry

func ShopRegistry() *Registry {
	if shopReg == nil {
		shopReg = buildShopRegistry()
	}
	return shopReg
}

func shopFrom(args map[string]any) (shop.Shop, error) {
	slots, err := argStr(args, "slots")
	if err != nil {
		return shop.Shop{}, err
	}
	sh := shop.Shop{Slots: ccg.ZoneName(slots)}
	if v, ok := args["stock"].(string); ok {
		sh.Stock = ccg.ZoneName(v)
	}
	if _, ok := args["size"]; ok {
		if n, err := argInt(args, "size"); err == nil {
			sh.Size = n
		}
	}
	return sh, nil
}

func itemArg(args map[string]any) (ccg.EntityID, error) {
	tok, err := argStr(args, "item")
	if err != nil {
		return 0, err
	}
	return ParseEntityToken(tok)
}

func buildShopRegistry() *Registry {
	r := NewRegistry()

	r.Add(Op{Module: "shop", Name: "clear", MCPTool: "shop_clear",
		Call: func(modules map[string]any, args map[string]any, rng *core.Random) (any, error) {
			s, err := ccgFrom(modules)
			if err != nil {
				return nil, err
			}
			sh, err := shopFrom(args)
			if err != nil {
				return nil, err
			}
			dest, _ := args["dest"].(string)
			return nil, sh.Clear(s, ccg.ZoneName(dest))
		}})

	r.Add(Op{Module: "shop", Name: "fill", MCPTool: "shop_fill",
		Call: func(modules map[string]any, args map[string]any, rng *core.Random) (any, error) {
			s, err := ccgFrom(modules)
			if err != nil {
				return nil, err
			}
			sh, err := shopFrom(args)
			if err != nil {
				return nil, err
			}
			return nil, sh.Fill(s, nil)
		}})

	r.Add(Op{Module: "shop", Name: "roll", MCPTool: "shop_roll",
		Call: func(modules map[string]any, args map[string]any, rng *core.Random) (any, error) {
			s, err := ccgFrom(modules)
			if err != nil {
				return nil, err
			}
			sh, err := shopFrom(args)
			if err != nil {
				return nil, err
			}
			dest, _ := args["dest"].(string)
			return nil, sh.Roll(s, nil, ccg.ZoneName(dest))
		}})

	r.Add(Op{Module: "shop", Name: "buy", MCPTool: "shop_buy",
		Call: func(modules map[string]any, args map[string]any, rng *core.Random) (any, error) {
			s, err := ccgFrom(modules)
			if err != nil {
				return nil, err
			}
			sh, err := shopFrom(args)
			if err != nil {
				return nil, err
			}
			item, err := itemArg(args)
			if err != nil {
				return nil, err
			}
			dest, err := argStr(args, "dest")
			if err != nil {
				return nil, err
			}
			return nil, sh.Buy(s, item, ccg.ZoneName(dest))
		}})

	r.Add(Op{Module: "shop", Name: "freeze", MCPTool: "shop_freeze",
		Call: func(modules map[string]any, args map[string]any, rng *core.Random) (any, error) {
			s, err := ccgFrom(modules)
			if err != nil {
				return nil, err
			}
			sh, err := shopFrom(args)
			if err != nil {
				return nil, err
			}
			item, err := itemArg(args)
			if err != nil {
				return nil, err
			}
			return nil, sh.Freeze(s, item)
		}})

	r.Add(Op{Module: "shop", Name: "unfreeze", MCPTool: "shop_unfreeze",
		Call: func(modules map[string]any, args map[string]any, rng *core.Random) (any, error) {
			s, err := ccgFrom(modules)
			if err != nil {
				return nil, err
			}
			item, err := itemArg(args)
			if err != nil {
				return nil, err
			}
			return nil, shop.Shop{}.Unfreeze(s, item)
		}})

	r.Add(Op{Module: "shop", Name: "is_frozen", MCPTool: "shop_is_frozen", ReadOnly: true,
		Call: func(modules map[string]any, args map[string]any, rng *core.Random) (any, error) {
			s, err := ccgFrom(modules)
			if err != nil {
				return nil, err
			}
			item, err := itemArg(args)
			if err != nil {
				return nil, err
			}
			return shop.Shop{}.IsFrozen(s, item), nil
		}})

	return r
}
