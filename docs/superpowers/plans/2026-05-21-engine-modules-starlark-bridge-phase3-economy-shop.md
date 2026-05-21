# Engine Modules in Starlark — Phase 3 (economy + shop) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let Starlark games use the engine's `modules/economy` (per-player resource pools) and `modules/shop` (refreshable market) via `ctx.modules.economy.*` / `ctx.modules.shop.*`. Because both modules are stateless logic *over* `ccg.State`, this phase first generalizes the bridge so an op can reach other modules' live state.

**Architecture:** Phases 1–2 gave each module its OWN live state and `Op.Call(state any, …)` received just that one state. economy and shop hold no state of their own — they are config+logic over a `*ccg.State` (counters for economy; zones for shop). So phase 3's enabling change is widening `Op.Call` to receive the full `map[string]any` of live module states, letting composing modules pull `modules["ccg"]`. ccg and tabletop ops are updated to pull their own state from the map (mechanical). economy/shop register a trivial presence in `StarlarkG.Modules` (so their `ctx.modules.<name>` binding exists) and operate on the ccg state. Declaring `economy` or `shop` without `ccg` is rejected at load time.

**Tech Stack:** Go, `go.starlark.net`, `modules/economy`, `modules/shop`, `modules/ccg`, the phase-1/2 `mcp/modulebridge` + `mcp/starlarkgame` machinery.

**Reference spec:** `docs/superpowers/specs/2026-05-21-engine-modules-in-starlark-and-mcp.md` (phase 3).

**Scope note:** shop stock *shuffling* needs the seeded RNG, which the bridge doesn't thread to ops yet (same deferred RNG-determinism work noted in phase 2 for combat/dice). `shop.Fill`/`Roll` accept a `*core.Random` but do not consume it, so we pass `nil`; designers arrange Stock order explicitly (or via a future ccg `shuffle` op). Do NOT wire RNG here.

**What phases 1–2 already give us (reuse, do not rebuild):**
- `mcp/modulebridge`: `Op`, `Registry` (`Add`/`Ops`/`Modules`), `NewRegistry`, package maps `stateFactories` / `registryByName`, `NewState` / `RegistryFor`, `EntityToken`/`ParseEntityToken`, arg helpers `argStr`/`argInt`/`argBool`. Two module files `ccg.go`, `tabletop.go` show the `init()` registration pattern.
- `mcp/starlarkgame`: `StarlarkG{Data, Modules}`; `ctx.modules.<name>.<op>()` wiring in `bridge.go` `modulesAsStarlark` (GENERIC); Setup instantiation + Validate scan (GENERIC); `knownModules` whitelist in `spec.go`.
- `mcp/tools.go` `ModuleOp` (GENERIC dispatch).

---

## File Structure

- `mcp/modulebridge/registry.go` (modify) — widen `Op.Call` signature to `func(modules map[string]any, args map[string]any) (any, error)`; add a tiny `emptyState` type for stateless modules.
- `mcp/modulebridge/ccg.go` (modify) — ops pull `modules["ccg"]`.
- `mcp/modulebridge/tabletop.go` (modify) — ops pull `modules["tabletop"]`.
- `mcp/modulebridge/ccg_test.go`, `tabletop_test.go` (modify) — call sites pass a modules map.
- `mcp/starlarkgame/bridge.go` (modify) — `modulesAsStarlark` passes the whole `c.Modules` map to `op.Call`.
- `mcp/tools.go` (modify) — `ModuleOp` passes `sg.Modules` to `op.Call`.
- `mcp/modulebridge/economy.go` (new) + `economy_test.go` (new).
- `mcp/modulebridge/shop.go` (new) + `shop_test.go` (new).
- `mcp/starlarkgame/spec.go` (modify) — whitelist `economy`/`shop`; reject them without `ccg`.
- `mcp/starlarkgame/modules_economy_shop_test.go` (new) — Starlark integration (gold pool + shop buy) + replay.
- `mcp/docs/designed-games.md` (modify) — document economy + shop.

---

## Task 1: Generalize `Op.Call` to receive all modules

This is a mechanical, wide refactor. The safety net is that all existing ccg + tabletop tests must stay green.

**Files:** `mcp/modulebridge/registry.go`, `ccg.go`, `tabletop.go`, `ccg_test.go`, `tabletop_test.go`, `mcp/starlarkgame/bridge.go`, `mcp/tools.go`.

- [ ] **Step 1: Change the `Op.Call` signature in `registry.go`**

Change the field:
```go
type Op struct {
	Module  string
	Name    string
	MCPTool string
	// Call receives ALL live module states (keyed by module name) so a
	// composing module (economy/shop over ccg) can reach another's state.
	// An op pulls its own module via modules[op.Module].
	Call func(modules map[string]any, args map[string]any) (any, error)
}
```
Add the stateless-module marker (used by economy/shop in later tasks):
```go
// emptyState is the placeholder a stateless module (economy, shop) puts
// in StarlarkG.Modules so its ctx.modules.<name> binding exists. Such
// modules operate on another module's state (ccg), not their own.
type emptyState struct{}
```

- [ ] **Step 2: Update `ccg.go` ops to pull their own state from the map**

Replace the `asCCG(state any)` helper with one that reads the map, and update every op to take `modules` and call it. Add:
```go
func ccgFrom(modules map[string]any) (*ccg.State, error) {
	s, ok := modules["ccg"].(*ccg.State)
	if !ok {
		return nil, fmt.Errorf("modulebridge: ccg op needs ccg state, got %T", modules["ccg"])
	}
	return s, nil
}
```
In every `r.Add(Op{... Call: func(state any, args ...) ...})` change the closure signature to `func(modules map[string]any, args map[string]any) (any, error)` and replace the first line `s, err := asCCG(state)` with `s, err := ccgFrom(modules)`. Delete the old `asCCG` function. (The ops are otherwise unchanged.)

- [ ] **Step 3: Update `tabletop.go` ops the same way**

Add:
```go
func tabletopFrom(modules map[string]any) (*tabletopState, error) {
	s, ok := modules["tabletop"].(*tabletopState)
	if !ok {
		return nil, fmt.Errorf("modulebridge: tabletop op needs tabletop state, got %T", modules["tabletop"])
	}
	return s, nil
}
```
Change every tabletop op closure to `func(modules map[string]any, args map[string]any) (any, error)` and replace `s, err := asTabletop(state)` with `s, err := tabletopFrom(modules)`. Delete the old `asTabletop` function.

- [ ] **Step 4: Update `bridge.go` `modulesAsStarlark`**

Find the line that calls the op (currently `res, err := op.Call(st, m)` where `st := state` was the single module's state). Change the builtin to pass the whole modules map. Remove the `st := state` capture; the closure should capture the outer `c` (the BridgeCtx) and call:
```go
				res, err := op.Call(c.Modules, m)
```
(Keep `op := op` capture. The `for name, state := range c.Modules` loop no longer needs `state` for the call — it's still used to know which modules to expose, so keep iterating `c.Modules` but you may rename the unused value to `_`.)

- [ ] **Step 5: Update `tools.go` `ModuleOp`**

Currently it resolves `st, ok := sg.Modules[args.Module]` then `chosen.Call(st, args.Args)`. Change to verify the module is present in `sg.Modules` (so the guard "match did not declare module" still holds) but pass the whole map:
```go
	if _, ok := sg.Modules[args.Module]; !ok {
		return ModuleOpResult{}, fmt.Errorf("match %s did not declare module %q", args.MatchID, args.Module)
	}
	res, err := chosen.Call(sg.Modules, args.Args)
```

- [ ] **Step 6: Update existing test call sites in `ccg_test.go` and `tabletop_test.go`**

Every `op.Call(st, map[string]any{...})` must now pass a modules map. In `ccg_test.go` replace each `.Call(st, ` with `.Call(map[string]any{"ccg": st}, `. In `tabletop_test.go` replace each `.Call(st, ` with `.Call(map[string]any{"tabletop": st}, `. (The `st` variables stay as they are — `ccg.NewState()` / `NewState("tabletop")`.)

- [ ] **Step 7: Build and run the full suites — nothing should regress**

Run: `export PATH=$HOME/.local/go/bin:$PATH && cd mcp && go build ./... && go test ./modulebridge/ ./starlarkgame/ -v 2>&1 | tail -40`
Expected: ALL PASS (all ccg + tabletop unit tests, plus the starlarkgame integration tests which exercise the new `op.Call(c.Modules, …)` path). If a starlarkgame integration test fails, the `modulesAsStarlark` change is wrong — fix it, don't touch the tests.

- [ ] **Step 8: Commit**

```bash
cd mcp && git add modulebridge/registry.go modulebridge/ccg.go modulebridge/tabletop.go modulebridge/ccg_test.go modulebridge/tabletop_test.go starlarkgame/bridge.go tools.go
git commit -m "modulebridge: Op.Call receives all live modules (enables composing modules)"
```

---

## Task 2: economy op set

economy `Pool` is config `{Owner ccg.EntityID, Kind string, Cap int}` over ccg counters. Ops take `owner` (an entity token), `kind`, optional `cap`, and amounts as args, and operate on `modules["ccg"]`. `scaled` is a pure helper needing no state.

ccg API used (already exists): `(*ccg.State).Counters(id, kind) int`, `AddCounter`, `RemoveCounter`. economy API: `economy.Pool{Owner, Kind, Cap}` with `Current(s) int`, `Gain(s, n) int`, `Spend(s, n) error` (`economy.ErrInsufficient`, `economy.ErrNegativeSpend`), `Set(s, n) int`; `economy.Scaled(turn, base, per, max) int`.

**Files:** create `mcp/modulebridge/economy.go`, `mcp/modulebridge/economy_test.go`.

- [ ] **Step 1: Write `mcp/modulebridge/economy_test.go`**

```go
package modulebridge

import (
	"testing"

	"github.com/tjcran/boardgame-go/modules/ccg"
)

func ecoOp(t *testing.T, name string) Op {
	t.Helper()
	for _, op := range EconomyRegistry().Ops("economy") {
		if op.Name == name {
			return op
		}
	}
	t.Fatalf("op %q not registered", name)
	return Op{}
}

func TestEconomy_GainSpendCurrent(t *testing.T) {
	st := ccg.NewState()
	owner := st.NewEntity("player", "0", nil) // ent:1
	mods := map[string]any{"ccg": st, "economy": &emptyState{}}
	tok := EntityToken(owner)

	if _, err := ecoOp(t, "gain").Call(mods, map[string]any{"owner": tok, "kind": "gold", "cap": int64(10), "n": int64(6)}); err != nil {
		t.Fatalf("gain: %v", err)
	}
	cur, err := ecoOp(t, "current").Call(mods, map[string]any{"owner": tok, "kind": "gold"})
	if err != nil {
		t.Fatalf("current: %v", err)
	}
	if cur.(int64) != 6 {
		t.Fatalf("current = %v, want 6", cur)
	}
	if _, err := ecoOp(t, "spend").Call(mods, map[string]any{"owner": tok, "kind": "gold", "n": int64(4)}); err != nil {
		t.Fatalf("spend: %v", err)
	}
	cur2, _ := ecoOp(t, "current").Call(mods, map[string]any{"owner": tok, "kind": "gold"})
	if cur2.(int64) != 2 {
		t.Fatalf("after spend = %v, want 2", cur2)
	}
}

func TestEconomy_SpendInsufficient(t *testing.T) {
	st := ccg.NewState()
	owner := st.NewEntity("player", "0", nil)
	mods := map[string]any{"ccg": st, "economy": &emptyState{}}
	_, err := ecoOp(t, "spend").Call(mods, map[string]any{"owner": EntityToken(owner), "kind": "gold", "n": int64(3)})
	if err == nil {
		t.Fatal("expected insufficient-funds error")
	}
}

func TestEconomy_Scaled(t *testing.T) {
	mods := map[string]any{"ccg": ccg.NewState(), "economy": &emptyState{}}
	res, err := ecoOp(t, "scaled").Call(mods, map[string]any{"turn": int64(3), "base": int64(4), "per": int64(1), "max": int64(10)})
	if err != nil {
		t.Fatalf("scaled: %v", err)
	}
	if res.(int64) != 6 { // 4 + (3-1)*1 = 6
		t.Fatalf("scaled = %v, want 6", res)
	}
}
```

- [ ] **Step 2: Confirm failure** — `export PATH=$HOME/.local/go/bin:$PATH && cd mcp && go test ./modulebridge/ -run TestEconomy -v` → FAIL (EconomyRegistry undefined).

- [ ] **Step 3: Write `mcp/modulebridge/economy.go`**

```go
package modulebridge

import (
	"github.com/tjcran/boardgame-go/modules/ccg"
	"github.com/tjcran/boardgame-go/modules/economy"
)

func init() {
	stateFactories["economy"] = func() any { return &emptyState{} }
	registryByName["economy"] = EconomyRegistry()
}

var economyReg *Registry

// EconomyRegistry returns the registry holding the economy op set.
func EconomyRegistry() *Registry {
	if economyReg == nil {
		economyReg = buildEconomyRegistry()
	}
	return economyReg
}

// poolFrom builds an economy.Pool config from op args (owner token,
// kind, optional cap) and returns it with the ccg state to operate on.
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
	cap := 0
	if _, ok := args["cap"]; ok {
		if c, err := argInt(args, "cap"); err == nil {
			cap = c
		}
	}
	return economy.Pool{Owner: owner, Kind: kind, Cap: cap}, s, nil
}

func buildEconomyRegistry() *Registry {
	r := NewRegistry()

	r.Add(Op{Module: "economy", Name: "current", MCPTool: "economy_current",
		Call: func(modules map[string]any, args map[string]any) (any, error) {
			p, s, err := poolFrom(modules, args)
			if err != nil {
				return nil, err
			}
			return int64(p.Current(s)), nil
		}})

	r.Add(Op{Module: "economy", Name: "gain", MCPTool: "economy_gain",
		Call: func(modules map[string]any, args map[string]any) (any, error) {
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
		Call: func(modules map[string]any, args map[string]any) (any, error) {
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
		Call: func(modules map[string]any, args map[string]any) (any, error) {
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
		Call: func(modules map[string]any, args map[string]any) (any, error) {
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
			max := 0
			if _, ok := args["max"]; ok {
				if m, err := argInt(args, "max"); err == nil {
					max = m
				}
			}
			return int64(economy.Scaled(turn, base, per, max)), nil
		}})

	return r
}
```

- [ ] **Step 4: Confirm pass** — `... go test ./modulebridge/ -run TestEconomy -v` → PASS.

- [ ] **Step 5: Commit**
```bash
cd mcp && git add modulebridge/economy.go modulebridge/economy_test.go
git commit -m "modulebridge: economy op set (pools over ccg counters) + scaled helper"
```

---

## Task 3: shop op set

shop `Shop` is config `{Slots, Stock ccg.ZoneName; Size int}` over ccg zones. Ops reconstruct it from args and operate on `modules["ccg"]`. `fill`/`roll` pass `nil` for `*core.Random` (unused; see scope note).

shop API: `shop.Shop{Slots, Stock, Size}` with `Clear(s, dest) error`, `Fill(s, r) error`, `Roll(s, r, dest) error`, `Buy(s, item, dest) error`, `Freeze(s, id) error`, `Unfreeze(s, id) error`, `IsFrozen(s, id) bool`. Errors: `shop.ErrNotInSlots`, plus ccg zone errors.

**Files:** create `mcp/modulebridge/shop.go`, `mcp/modulebridge/shop_test.go`.

- [ ] **Step 1: Write `mcp/modulebridge/shop_test.go`**

```go
package modulebridge

import (
	"testing"

	"github.com/tjcran/boardgame-go/modules/ccg"
)

func shopOp(t *testing.T, name string) Op {
	t.Helper()
	for _, op := range ShopRegistry().Ops("shop") {
		if op.Name == name {
			return op
		}
	}
	t.Fatalf("op %q not registered", name)
	return Op{}
}

// buildShopState sets up ccg zones: a stock of 3 cards (ordered) and an
// empty slots row, returns the modules map + the card tokens.
func buildShopState(t *testing.T) (map[string]any, *ccg.State, []string) {
	t.Helper()
	st := ccg.NewState()
	st.NewZone("slots", false)
	st.NewZone("stock", true)
	st.NewZone("hand", false)
	toks := make([]string, 0, 3)
	for i := 0; i < 3; i++ {
		id := st.NewEntity("item", "", map[string]any{"cost": 1})
		if err := st.MoveTo(id, "stock"); err != nil {
			t.Fatalf("seed stock: %v", err)
		}
		toks = append(toks, EntityToken(id))
	}
	return map[string]any{"ccg": st, "shop": &emptyState{}}, st, toks
}

func TestShop_FillBuyFreeze(t *testing.T) {
	mods, st, _ := buildShopState(t)

	// Fill slots to size 2 from stock.
	if _, err := shopOp(t, "fill").Call(mods, map[string]any{"slots": "slots", "stock": "stock", "size": int64(2)}); err != nil {
		t.Fatalf("fill: %v", err)
	}
	if st.Size("slots") != 2 || st.Size("stock") != 1 {
		t.Fatalf("after fill: slots=%d stock=%d, want 2/1", st.Size("slots"), st.Size("stock"))
	}

	// Buy the first slot item into hand.
	first := EntityToken(st.Zones["slots"].Members[0])
	if _, err := shopOp(t, "buy").Call(mods, map[string]any{"slots": "slots", "item": first, "dest": "hand"}); err != nil {
		t.Fatalf("buy: %v", err)
	}
	if st.Size("hand") != 1 || st.Size("slots") != 1 {
		t.Fatalf("after buy: hand=%d slots=%d, want 1/1", st.Size("hand"), st.Size("slots"))
	}

	// Freeze the remaining slot item, then clear — it should survive.
	keep := EntityToken(st.Zones["slots"].Members[0])
	if _, err := shopOp(t, "freeze").Call(mods, map[string]any{"slots": "slots", "item": keep}); err != nil {
		t.Fatalf("freeze: %v", err)
	}
	frRes, _ := shopOp(t, "is_frozen").Call(mods, map[string]any{"item": keep})
	if frRes.(bool) != true {
		t.Fatalf("is_frozen = %v, want true", frRes)
	}
	if _, err := shopOp(t, "clear").Call(mods, map[string]any{"slots": "slots", "dest": "stock"}); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if st.Size("slots") != 1 {
		t.Fatalf("after clear with frozen: slots=%d, want 1", st.Size("slots"))
	}
}

func TestShop_BuyNotInSlots(t *testing.T) {
	mods, st, toks := buildShopState(t)
	// toks[0] is in stock, not slots — buying it must error.
	_ = st
	if _, err := shopOp(t, "buy").Call(mods, map[string]any{"slots": "slots", "item": toks[0], "dest": "hand"}); err == nil {
		t.Fatal("expected ErrNotInSlots")
	}
}
```

- [ ] **Step 2: Confirm failure** — `... go test ./modulebridge/ -run TestShop -v` → FAIL.

- [ ] **Step 3: Write `mcp/modulebridge/shop.go`**

```go
package modulebridge

import (
	"github.com/tjcran/boardgame-go/modules/ccg"
	"github.com/tjcran/boardgame-go/modules/shop"
)

func init() {
	stateFactories["shop"] = func() any { return &emptyState{} }
	registryByName["shop"] = ShopRegistry()
}

var shopReg *Registry

// ShopRegistry returns the registry holding the shop op set.
func ShopRegistry() *Registry {
	if shopReg == nil {
		shopReg = buildShopRegistry()
	}
	return shopReg
}

// shopFrom builds a shop.Shop config from args. slots is required; stock
// and size are read when present (fill/roll need them; buy/freeze/clear
// only need slots).
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
		Call: func(modules map[string]any, args map[string]any) (any, error) {
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
		Call: func(modules map[string]any, args map[string]any) (any, error) {
			s, err := ccgFrom(modules)
			if err != nil {
				return nil, err
			}
			sh, err := shopFrom(args)
			if err != nil {
				return nil, err
			}
			return nil, sh.Fill(s, nil) // r unused by Fill; see scope note
		}})

	r.Add(Op{Module: "shop", Name: "roll", MCPTool: "shop_roll",
		Call: func(modules map[string]any, args map[string]any) (any, error) {
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
		Call: func(modules map[string]any, args map[string]any) (any, error) {
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
		Call: func(modules map[string]any, args map[string]any) (any, error) {
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
		Call: func(modules map[string]any, args map[string]any) (any, error) {
			s, err := ccgFrom(modules)
			if err != nil {
				return nil, err
			}
			item, err := itemArg(args)
			if err != nil {
				return nil, err
			}
			// Unfreeze does not require Slots; an empty Shop config is fine.
			return nil, shop.Shop{}.Unfreeze(s, item)
		}})

	r.Add(Op{Module: "shop", Name: "is_frozen", MCPTool: "shop_is_frozen",
		Call: func(modules map[string]any, args map[string]any) (any, error) {
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
```

- [ ] **Step 4: Confirm pass** — `... go test ./modulebridge/ -run TestShop -v` → PASS. Then full package: `... go test ./modulebridge/` → ok.

- [ ] **Step 5: Commit**
```bash
cd mcp && git add modulebridge/shop.go modulebridge/shop_test.go
git commit -m "modulebridge: shop op set (refreshable market over ccg zones)"
```

---

## Task 4: whitelist + dependency check + Starlark integration

**Files:** `mcp/starlarkgame/spec.go`, create `mcp/starlarkgame/modules_economy_shop_test.go`.

- [ ] **Step 1: Whitelist economy/shop and require ccg, in `spec.go`**

Change `var knownModules = map[string]bool{"ccg": true, "tabletop": true}` to add `"economy": true, "shop": true`. Then add a dependency check inside `readModules`, after the loop that fills `s.Modules`, before `return nil`:
```go
	declared := map[string]bool{}
	for _, m := range s.Modules {
		declared[m] = true
	}
	for _, m := range s.Modules {
		if (m == "economy" || m == "shop") && !declared["ccg"] {
			return fmt.Errorf("MODULES: %q requires \"ccg\" to also be declared", m)
		}
	}
```

- [ ] **Step 2: Write the integration test `mcp/starlarkgame/modules_economy_shop_test.go`**

```go
package starlarkgame

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/match"
	"github.com/tjcran/boardgame-go/storage"
)

// A 1-player mini auto-battler turn: setup gives the player 3 gold and a
// shop of 2 items (cost 1) drawn from a 3-item stock. The "buy_first"
// move buys the first slot item into hand and pays its cost.
const miniShopSpec = `
META = {"name": "mini-shop", "min_players": 1, "max_players": 1}
MODULES = ["ccg", "economy", "shop"]

def setup(ctx):
    p = ctx.modules.ccg.new_entity(type="player", owner="0")
    ctx.modules.economy.set(owner=p, kind="gold", cap=10, n=3)
    ctx.modules.ccg.new_zone(name="slots", ordered=False)
    ctx.modules.ccg.new_zone(name="stock", ordered=True)
    ctx.modules.ccg.new_zone(name="hand", ordered=False)
    for i in range(3):
        c = ctx.modules.ccg.new_entity(type="item", owner="")
        ctx.modules.ccg.move_to(entity=c, zone="stock")
    ctx.modules.shop.fill(slots="slots", stock="stock", size=2)
    return {"player": p}

def end_if(state, ctx): return None
def legal_moves(state, ctx): return [{"name": "buy_first", "args": []}]

def buy_first(state, ctx):
    p = state["player"]
    item = ctx.modules.ccg.members(zone="slots")[0]
    ctx.modules.economy.spend(owner=p, kind="gold", n=1)
    ctx.modules.shop.buy(slots="slots", item=item, dest="hand")
    return state

MOVES = {"buy_first": {"apply": buy_first}}
`

func TestMiniShop_SetupBuyReplay(t *testing.T) {
	spec, err := LoadSpec(miniShopSpec)
	if err != nil {
		t.Fatalf("LoadSpec: %v", err)
	}
	if err := Validate(context.Background(), spec); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	g := BuildCoreGame(spec)

	mgr := match.NewManager(storage.NewMemory())
	mgr.MustRegister(g)
	id, err := mgr.Create("mini-shop", match.CreateOptions{NumPlayers: 1})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	jr, err := mgr.Join(id, "p", match.JoinOptions{})
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	if _, err := mgr.MoveReq(id, jr.PlayerID, jr.PlayerCredentials, core.MoveRequest{Move: "buy_first"}); err != nil {
		t.Fatalf("buy_first: %v", err)
	}

	replayed, err := core.Replay(g, st(mgr, id).State.Log, 1, nil)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	orig, _ := json.Marshal(st(mgr, id).State.G)
	repl, _ := json.Marshal(replayed.G)
	if string(orig) != string(repl) {
		t.Fatalf("replay mismatch:\n orig=%s\n repl=%s", orig, repl)
	}
}

func st(mgr *match.Manager, id string) *storage.Match {
	m, _ := mgr.State(id)
	return m
}
```
If the helper name `st` collides with an existing symbol in the package's test files, rename it (e.g. `matchState`). Cross-check `modules_replay_test.go` for the real `core.Replay` signature (it is `core.Replay(g, log, numPlayers, setupData) (core.State, error)`).

- [ ] **Step 3: Run it** — `export PATH=$HOME/.local/go/bin:$PATH && cd mcp && go test ./starlarkgame/ -run TestMiniShop -v`. Same outcomes/handling as prior phases. If the move errors on `economy.spend` insufficiency, the setup `economy.set` didn't apply — debug, don't weaken.

- [ ] **Step 4: Full suites** — `... go test ./starlarkgame/ ./modulebridge/` → ok / ok.

- [ ] **Step 5: Commit**
```bash
cd mcp && git add starlarkgame/spec.go starlarkgame/modules_economy_shop_test.go
git commit -m "starlarkgame: whitelist economy/shop (require ccg) + integration test"
```

---

## Task 5: Documentation

**Files:** `mcp/docs/designed-games.md`.

- [ ] **Step 1: Read** the existing "Using engine modules (MODULES)" section (ccg + tabletop subsections).

- [ ] **Step 2: Add an economy + shop subsection** after the tabletop one:

```markdown
### economy + shop modules (resource/market games)

`economy` and `shop` are stateless logic over `ccg`, so declaring either REQUIRES
`ccg` in MODULES (`MODULES = ["ccg", "economy", "shop"]`). economy operates on
ccg counters for a per-player resource; shop manages a refreshable market over
two ccg zones.

    MODULES = ["ccg", "economy", "shop"]

    def setup(ctx):
        p = ctx.modules.ccg.new_entity(type="player", owner="0")
        ctx.modules.economy.set(owner=p, kind="gold", cap=10, n=3)
        ctx.modules.ccg.new_zone(name="slots", ordered=False)
        ctx.modules.ccg.new_zone(name="stock", ordered=True)
        ctx.modules.ccg.new_zone(name="hand", ordered=False)
        # … seed stock with items …
        ctx.modules.shop.fill(slots="slots", stock="stock", size=5)
        return {"player": p}

    def buy(state, ctx):
        item = ctx.modules.ccg.members(zone="slots")[0]
        ctx.modules.economy.spend(owner=state["player"], kind="gold", n=1)
        ctx.modules.shop.buy(slots="slots", item=item, dest="hand")
        return state

economy ops: `current(owner, kind)`, `gain(owner, kind, cap, n)` → applied delta,
`spend(owner, kind, n)` (errors if insufficient), `set(owner, kind, cap, n)` →
final value, `scaled(turn, base, per, max)` → per-turn income. `owner` is a ccg
entity token.

shop ops: `fill(slots, stock, size)`, `roll(slots, stock, size, dest)`,
`clear(slots, dest)`, `buy(slots, item, dest)`, `freeze(slots, item)`,
`unfreeze(item)`, `is_frozen(item)`. Items are ccg entity tokens. Stock is drawn
in order (no shuffle yet — arrange Stock as you want it dealt). `buy` does not
charge — pair it with `economy.spend` as above.

target selection and event hooks land in later phases.
```
(Update the trailing "land in later phases" line in the ccg/tabletop area if it now double-lists; keep one accurate trailer.)

- [ ] **Step 3: Build + full test + commit**
```bash
export PATH=$HOME/.local/go/bin:$PATH && cd mcp && go build ./... && go test ./modulebridge/ ./starlarkgame/
```
Expected: clean + green.
```bash
cd mcp && git add docs/designed-games.md && git commit -m "docs: document economy + shop modules for designed games"
```

---

## Self-Review

**Spec coverage:** Phase-3 scope (economy + shop) delivered. The composing-module access problem is solved generically (Task 1) rather than special-cased, so future composing modules reuse it. ✓

**Reuse validation:** After Task 1's signature change, economy/shop are pure additions (new files + whitelist + dep check). The `ctx.modules` wiring, Setup, Validate scan, and `module_op` tool stay generic. ✓

**Placeholder scan:** No TBD/TODO. Conditional debug guidance in Task 4 has concrete steps. ✓

**Type consistency:** `Op.Call(modules map[string]any, args map[string]any)` is used uniformly. `ccgFrom`/`tabletopFrom`/`poolFrom`/`shopFrom`/`itemArg` helpers and op names (`current/gain/spend/set/scaled`, `clear/fill/roll/buy/freeze/unfreeze/is_frozen`) are consistent across registration, tests, docs. economy/shop factories return `&emptyState{}`. ✓

**Determinism:** economy ops are pure over counters; shop ops are pure over zones (RNG param passed nil, unused). No wall-clock/unseeded randomness. Replay proven in Task 4. ✓

**Known caveats (carry forward):** (1) shop stock shuffling needs seeded-RNG wiring — deferred (shared with phase-2 combat/dice). (2) economy/shop `emptyState` markers serialize as `{}` under `__modules__`; harmless. (3) RNG-to-ops threading remains the main unsolved cross-cutting item before phases needing dice/shuffle.
```
