# Engine Modules in Starlark — Phase 2 (tabletop) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let Starlark-authored games use the engine's `modules/tabletop` package (square/hex boards, unit positions, terrain, line-of-sight) through `ctx.modules.tabletop.*`, reusing the phase-1 module-bridge mechanism unchanged.

**Architecture:** Phase 1 (PR #60) built a generic bridge: a `mcp/modulebridge` registry whose ops are exposed both as `ctx.modules.<name>.<op>()` Starlark builtins and as the `module_op` MCP tool, with module states held live in `StarlarkG.Modules` across moves. Phase 2 adds a `tabletop` binding to that registry. Unlike `ccg` (a single `*ccg.State`), tabletop needs three live objects, so the module state is a small **bundle** struct `{Space *tabletop.State; Board tabletop.Board; Terrain *tabletop.TerrainMap}`. The board is chosen at setup via a `new_board` op (not encoded in the module name). Positions cross the boundary as plain `(x, y)` integers and units as plain integer ids — no opaque handles needed (positions/units are not opaque the way ccg entities are).

**Tech Stack:** Go, `go.starlark.net`, `modules/tabletop`, the phase-1 `mcp/modulebridge` + `mcp/starlarkgame` machinery.

**Reference spec:** `docs/superpowers/specs/2026-05-21-engine-modules-in-starlark-and-mcp.md` (phase 2 in the "Implementation phasing" section).

**Scope note:** Phase 2 covers spatial geometry + terrain + line-of-sight. The `modules/tabletop` dice/combat chain (`combat.go`) is DEFERRED — it consumes RNG and needs determinism wiring through the seeded `plugins/random`, which is its own follow-up. Do not implement combat here.

**What phase 1 already gives us (do NOT rebuild):**
- `mcp/modulebridge/registry.go`: `Op{Module,Name,MCPTool string; Call func(state any, args map[string]any)(any,error)}`, `(*Registry).Add/Ops/Modules`, `NewRegistry()`, the package-level maps `stateFactories map[string]func() any` and `registryByName map[string]*Registry`, and `NewState(module string) any` / `RegistryFor(name string) *Registry`.
- `mcp/modulebridge/ccg.go`: the pattern to copy — an `init()` that registers `stateFactories["ccg"]` and `registryByName["ccg"]`, a cached `CCGRegistry()`, arg helpers `argStr/argBool/argInt`, and a `buildCCGRegistry()` adding ops.
- `mcp/starlarkgame/bridge.go` `modulesAsStarlark()`: GENERIC — iterates `RegistryFor(name).Ops(name)` for every module in `BridgeCtx.Modules`. No change needed for a new module.
- `mcp/starlarkgame/game.go` Setup + `mcp/starlarkgame/validate.go`: GENERIC — instantiate every declared module via `modulebridge.NewState(name)`. No change needed.
- `mcp/tools.go` `ModuleOp`: GENERIC — resolves `sg.Modules[module]` and calls the registry op. No change needed.
- `mcp/starlarkgame/spec.go` `knownModules` whitelist: currently `{"ccg": true}` — phase 2 adds `"tabletop"`.

---

## File Structure

- `mcp/modulebridge/tabletop.go` (new) — the bundle type, state factory, registry registration, and the tabletop op set. Mirrors `ccg.go`.
- `mcp/modulebridge/tabletop_test.go` (new) — registry-level unit tests for every op.
- `mcp/starlarkgame/spec.go` (modify) — add `"tabletop"` to `knownModules`.
- `mcp/starlarkgame/modules_tabletop_test.go` (new) — Starlark integration: a mini hex-skirmish spec runs end to end, plus a move-across-moves/replay determinism check.
- `mcp/docs/designed-games.md` (modify) — document the `tabletop` module.

Note the arg helpers (`argStr`, `argInt`, `argBool`) already live in `ccg.go` in package `modulebridge`; `tabletop.go` is in the same package and reuses them directly — do NOT redefine them.

---

## Task 1: tabletop bundle, state factory, whitelist, and `new_board`

**Files:**
- Create: `mcp/modulebridge/tabletop.go`
- Create: `mcp/modulebridge/tabletop_test.go`
- Modify: `mcp/starlarkgame/spec.go` (the `knownModules` map)

The bundle holds the three live tabletop objects. The factory creates `Space` and `Terrain` eagerly and leaves `Board` nil; `new_board` sets it. Ops needing the board error clearly when it is nil.

- [ ] **Step 1: Write the failing test `mcp/modulebridge/tabletop_test.go`**

```go
package modulebridge

import "testing"

func ttOp(t *testing.T, name string) Op {
	t.Helper()
	for _, op := range TabletopRegistry().Ops("tabletop") {
		if op.Name == name {
			return op
		}
	}
	t.Fatalf("op %q not registered", name)
	return Op{}
}

func TestTabletop_StateFactory(t *testing.T) {
	st := NewState("tabletop")
	if _, ok := st.(*tabletopState); !ok {
		t.Fatalf("NewState(tabletop) = %T, want *tabletopState", st)
	}
}

func TestTabletop_NewBoard_Square(t *testing.T) {
	st := NewState("tabletop")
	if _, err := ttOp(t, "new_board").Call(st, map[string]any{"kind": "square", "w": int64(8), "h": int64(8)}); err != nil {
		t.Fatalf("new_board: %v", err)
	}
	res, err := ttOp(t, "distance").Call(st, map[string]any{"ax": int64(0), "ay": int64(0), "bx": int64(3), "by": int64(2)})
	if err != nil {
		t.Fatalf("distance: %v", err)
	}
	if res.(int64) != 3 { // Chebyshev max(3,2)=3
		t.Fatalf("square distance = %v, want 3", res)
	}
}

func TestTabletop_NewBoard_RejectsUnknownKind(t *testing.T) {
	st := NewState("tabletop")
	if _, err := ttOp(t, "new_board").Call(st, map[string]any{"kind": "triangular", "w": int64(4), "h": int64(4)}); err == nil {
		t.Fatal("expected error for unknown board kind")
	}
}

func TestTabletop_DistanceWithoutBoard_Errors(t *testing.T) {
	st := NewState("tabletop")
	if _, err := ttOp(t, "distance").Call(st, map[string]any{"ax": int64(0), "ay": int64(0), "bx": int64(1), "by": int64(1)}); err == nil {
		t.Fatal("expected error: distance before new_board")
	}
}
```

- [ ] **Step 2: Run to confirm failure**

Run: `export PATH=$HOME/.local/go/bin:$PATH && go test ./modulebridge/ -run TestTabletop -v`
Expected: FAIL — `TabletopRegistry` / `tabletopState` undefined.

- [ ] **Step 3: Create `mcp/modulebridge/tabletop.go` (bundle, factory, registration, `new_board`, `distance`)**

```go
package modulebridge

import (
	"fmt"

	"github.com/tjcran/boardgame-go/modules/tabletop"
)

// tabletopState bundles the three live tabletop objects a spatial game
// needs. Board is nil until new_board runs; ops needing geometry error
// clearly until then. JSON note: Space and Terrain marshal cleanly;
// Board is an interface and marshals as its concrete struct fields
// (Width/Height) without a type tag — fine for the in-memory store
// (modules are not round-tripped between moves) and for client views.
type tabletopState struct {
	Space   *tabletop.State    `json:"space"`
	Board   tabletop.Board     `json:"board,omitempty"`
	Terrain *tabletop.TerrainMap `json:"terrain"`
}

func newTabletopState() *tabletopState {
	return &tabletopState{
		Space:   tabletop.NewState(),
		Terrain: tabletop.NewTerrainMap(),
	}
}

func init() {
	stateFactories["tabletop"] = func() any { return newTabletopState() }
	registryByName["tabletop"] = TabletopRegistry()
}

var tabletopReg *Registry

// TabletopRegistry returns the registry holding the tabletop op set.
func TabletopRegistry() *Registry {
	if tabletopReg == nil {
		tabletopReg = buildTabletopRegistry()
	}
	return tabletopReg
}

func asTabletop(state any) (*tabletopState, error) {
	s, ok := state.(*tabletopState)
	if !ok {
		return nil, fmt.Errorf("modulebridge: tabletop op got %T, want *tabletopState", state)
	}
	return s, nil
}

// pos reads {xKey, yKey} int args into a tabletop.Pos.
func pos(args map[string]any, xKey, yKey string) (tabletop.Pos, error) {
	x, err := argInt(args, xKey)
	if err != nil {
		return tabletop.Pos{}, err
	}
	y, err := argInt(args, yKey)
	if err != nil {
		return tabletop.Pos{}, err
	}
	return tabletop.Pos{X: x, Y: y}, nil
}

func posList(p tabletop.Pos) []any { return []any{int64(p.X), int64(p.Y)} }

func buildTabletopRegistry() *Registry {
	r := NewRegistry()

	r.Add(Op{Module: "tabletop", Name: "new_board", MCPTool: "tabletop_new_board",
		Call: func(state any, args map[string]any) (any, error) {
			s, err := asTabletop(state)
			if err != nil {
				return nil, err
			}
			kind, err := argStr(args, "kind")
			if err != nil {
				return nil, err
			}
			w, err := argInt(args, "w")
			if err != nil {
				return nil, err
			}
			h, err := argInt(args, "h")
			if err != nil {
				return nil, err
			}
			switch kind {
			case "square":
				s.Board = tabletop.NewSquareBoard(w, h)
			case "hex":
				s.Board = tabletop.NewHexBoard(w, h)
			default:
				return nil, fmt.Errorf("tabletop.new_board: unknown kind %q (want \"square\" or \"hex\")", kind)
			}
			return nil, nil
		}})

	r.Add(Op{Module: "tabletop", Name: "distance", MCPTool: "tabletop_distance",
		Call: func(state any, args map[string]any) (any, error) {
			s, err := asTabletop(state)
			if err != nil {
				return nil, err
			}
			if s.Board == nil {
				return nil, fmt.Errorf("tabletop.distance: no board; call new_board first")
			}
			a, err := pos(args, "ax", "ay")
			if err != nil {
				return nil, err
			}
			b, err := pos(args, "bx", "by")
			if err != nil {
				return nil, err
			}
			return int64(s.Board.Distance(a, b)), nil
		}})

	return r
}
```

- [ ] **Step 4: Add `"tabletop"` to the whitelist in `mcp/starlarkgame/spec.go`**

Change `var knownModules = map[string]bool{"ccg": true}` to:
```go
var knownModules = map[string]bool{"ccg": true, "tabletop": true}
```

- [ ] **Step 5: Run to confirm pass**

Run: `export PATH=$HOME/.local/go/bin:$PATH && go test ./modulebridge/ -run TestTabletop -v`
Expected: PASS (all four tests).

- [ ] **Step 6: Commit**

```bash
git add modulebridge/tabletop.go modulebridge/tabletop_test.go starlarkgame/spec.go
git commit -m "modulebridge: tabletop bundle, factory, new_board + distance; whitelist tabletop"
```

---

## Task 2: spatial, geometry, and terrain ops

**Files:**
- Modify: `mcp/modulebridge/tabletop.go` (add ops to `buildTabletopRegistry`)
- Modify: `mcp/modulebridge/tabletop_test.go` (add tests)

Add the rest of the op set: `place`, `move`, `remove`, `position_of`, `entities_at`, `within`, `neighbors`, `line_of_sight`, `tag_terrain`, `has_terrain`. Units are plain integer ids; positions are `[x, y]` lists out, `x`/`y` int args in.

- [ ] **Step 1: Append failing tests to `mcp/modulebridge/tabletop_test.go`**

```go
func TestTabletop_PlaceMovePositionEntities(t *testing.T) {
	st := NewState("tabletop")
	ttOp(t, "new_board").Call(st, map[string]any{"kind": "square", "w": int64(8), "h": int64(8)})

	if _, err := ttOp(t, "place").Call(st, map[string]any{"unit": int64(1), "x": int64(2), "y": int64(3)}); err != nil {
		t.Fatalf("place: %v", err)
	}
	posRes, err := ttOp(t, "position_of").Call(st, map[string]any{"unit": int64(1)})
	if err != nil {
		t.Fatalf("position_of: %v", err)
	}
	xy := posRes.([]any)
	if xy[0].(int64) != 2 || xy[1].(int64) != 3 {
		t.Fatalf("position_of = %v, want [2 3]", xy)
	}
	// position_of for an un-placed unit returns nil (None in Starlark).
	none, err := ttOp(t, "position_of").Call(st, map[string]any{"unit": int64(99)})
	if err != nil {
		t.Fatalf("position_of(99): %v", err)
	}
	if none != nil {
		t.Fatalf("position_of(99) = %v, want nil", none)
	}

	ttOp(t, "move").Call(st, map[string]any{"unit": int64(1), "x": int64(4), "y": int64(4)})
	atRes, err := ttOp(t, "entities_at").Call(st, map[string]any{"x": int64(4), "y": int64(4)})
	if err != nil {
		t.Fatalf("entities_at: %v", err)
	}
	at := atRes.([]any)
	if len(at) != 1 || at[0].(int64) != 1 {
		t.Fatalf("entities_at(4,4) = %v, want [1]", at)
	}

	if _, err := ttOp(t, "remove").Call(st, map[string]any{"unit": int64(1)}); err != nil {
		t.Fatalf("remove: %v", err)
	}
	atRes2, _ := ttOp(t, "entities_at").Call(st, map[string]any{"x": int64(4), "y": int64(4)})
	if len(atRes2.([]any)) != 0 {
		t.Fatalf("entities_at after remove = %v, want []", atRes2)
	}
}

func TestTabletop_NeighborsWithinLOSTerrain(t *testing.T) {
	st := NewState("tabletop")
	ttOp(t, "new_board").Call(st, map[string]any{"kind": "square", "w": int64(8), "h": int64(8)})

	// neighbors of a center cell on a square board = 8 king-moves.
	nRes, err := ttOp(t, "neighbors").Call(st, map[string]any{"x": int64(4), "y": int64(4)})
	if err != nil {
		t.Fatalf("neighbors: %v", err)
	}
	if len(nRes.([]any)) != 8 {
		t.Fatalf("neighbors count = %d, want 8", len(nRes.([]any)))
	}

	// within radius 1 of (4,4): place a unit at (5,4) -> in range; (0,0) -> out.
	ttOp(t, "place").Call(st, map[string]any{"unit": int64(1), "x": int64(5), "y": int64(4)})
	ttOp(t, "place").Call(st, map[string]any{"unit": int64(2), "x": int64(0), "y": int64(0)})
	wRes, err := ttOp(t, "within").Call(st, map[string]any{"x": int64(4), "y": int64(4), "radius": int64(1)})
	if err != nil {
		t.Fatalf("within: %v", err)
	}
	w := wRes.([]any)
	if len(w) != 1 || w[0].(int64) != 1 {
		t.Fatalf("within(4,4,1) = %v, want [1]", w)
	}

	// LOS: clear by default; tag (4,3) as blocking and check a vertical line (4,2)->(4,4).
	losRes, _ := ttOp(t, "line_of_sight").Call(st, map[string]any{"fx": int64(4), "fy": int64(2), "tx": int64(4), "ty": int64(4)})
	if losRes.(bool) != true {
		t.Fatalf("LOS before terrain = %v, want true", losRes)
	}
	if _, err := ttOp(t, "tag_terrain").Call(st, map[string]any{"x": int64(4), "y": int64(3), "tag": "blocks_los"}); err != nil {
		t.Fatalf("tag_terrain: %v", err)
	}
	hasRes, _ := ttOp(t, "has_terrain").Call(st, map[string]any{"x": int64(4), "y": int64(3), "tag": "blocks_los"})
	if hasRes.(bool) != true {
		t.Fatalf("has_terrain = %v, want true", hasRes)
	}
	losRes2, _ := ttOp(t, "line_of_sight").Call(st, map[string]any{"fx": int64(4), "fy": int64(2), "tx": int64(4), "ty": int64(4)})
	if losRes2.(bool) != false {
		t.Fatalf("LOS through blocker = %v, want false", losRes2)
	}
}
```

- [ ] **Step 2: Run to confirm failure**

Run: `export PATH=$HOME/.local/go/bin:$PATH && go test ./modulebridge/ -run TestTabletop -v`
Expected: FAIL — `place`/`neighbors`/etc. not registered.

- [ ] **Step 3: Add the ops to `buildTabletopRegistry` (before `return r`)**

```go
	r.Add(Op{Module: "tabletop", Name: "place", MCPTool: "tabletop_place",
		Call: func(state any, args map[string]any) (any, error) {
			s, err := asTabletop(state)
			if err != nil {
				return nil, err
			}
			unit, err := argInt(args, "unit")
			if err != nil {
				return nil, err
			}
			p, err := pos(args, "x", "y")
			if err != nil {
				return nil, err
			}
			s.Space.Place(tabletop.UnitID(unit), p)
			return nil, nil
		}})

	r.Add(Op{Module: "tabletop", Name: "move", MCPTool: "tabletop_move",
		Call: func(state any, args map[string]any) (any, error) {
			s, err := asTabletop(state)
			if err != nil {
				return nil, err
			}
			unit, err := argInt(args, "unit")
			if err != nil {
				return nil, err
			}
			p, err := pos(args, "x", "y")
			if err != nil {
				return nil, err
			}
			s.Space.Move(tabletop.UnitID(unit), p)
			return nil, nil
		}})

	r.Add(Op{Module: "tabletop", Name: "remove", MCPTool: "tabletop_remove",
		Call: func(state any, args map[string]any) (any, error) {
			s, err := asTabletop(state)
			if err != nil {
				return nil, err
			}
			unit, err := argInt(args, "unit")
			if err != nil {
				return nil, err
			}
			s.Space.Remove(tabletop.UnitID(unit))
			return nil, nil
		}})

	r.Add(Op{Module: "tabletop", Name: "position_of", MCPTool: "tabletop_position_of",
		Call: func(state any, args map[string]any) (any, error) {
			s, err := asTabletop(state)
			if err != nil {
				return nil, err
			}
			unit, err := argInt(args, "unit")
			if err != nil {
				return nil, err
			}
			p, ok := s.Space.PositionOf(tabletop.UnitID(unit))
			if !ok {
				return nil, nil
			}
			return posList(p), nil
		}})

	r.Add(Op{Module: "tabletop", Name: "entities_at", MCPTool: "tabletop_entities_at",
		Call: func(state any, args map[string]any) (any, error) {
			s, err := asTabletop(state)
			if err != nil {
				return nil, err
			}
			p, err := pos(args, "x", "y")
			if err != nil {
				return nil, err
			}
			units := s.Space.EntitiesAt(p)
			out := make([]any, 0, len(units))
			for _, u := range units {
				out = append(out, int64(u))
			}
			return out, nil
		}})

	r.Add(Op{Module: "tabletop", Name: "within", MCPTool: "tabletop_within",
		Call: func(state any, args map[string]any) (any, error) {
			s, err := asTabletop(state)
			if err != nil {
				return nil, err
			}
			if s.Board == nil {
				return nil, fmt.Errorf("tabletop.within: no board; call new_board first")
			}
			center, err := pos(args, "x", "y")
			if err != nil {
				return nil, err
			}
			radius, err := argInt(args, "radius")
			if err != nil {
				return nil, err
			}
			units := s.Space.Within(s.Board, center, radius)
			out := make([]any, 0, len(units))
			for _, u := range units {
				out = append(out, int64(u))
			}
			return out, nil
		}})

	r.Add(Op{Module: "tabletop", Name: "neighbors", MCPTool: "tabletop_neighbors",
		Call: func(state any, args map[string]any) (any, error) {
			s, err := asTabletop(state)
			if err != nil {
				return nil, err
			}
			if s.Board == nil {
				return nil, fmt.Errorf("tabletop.neighbors: no board; call new_board first")
			}
			p, err := pos(args, "x", "y")
			if err != nil {
				return nil, err
			}
			cells := s.Board.Neighbors(p)
			out := make([]any, 0, len(cells))
			for _, c := range cells {
				out = append(out, posList(c))
			}
			return out, nil
		}})

	r.Add(Op{Module: "tabletop", Name: "line_of_sight", MCPTool: "tabletop_line_of_sight",
		Call: func(state any, args map[string]any) (any, error) {
			s, err := asTabletop(state)
			if err != nil {
				return nil, err
			}
			if s.Board == nil {
				return nil, fmt.Errorf("tabletop.line_of_sight: no board; call new_board first")
			}
			from, err := pos(args, "fx", "fy")
			if err != nil {
				return nil, err
			}
			target, err := pos(args, "tx", "ty")
			if err != nil {
				return nil, err
			}
			clear := tabletop.LineOfSight(s.Board, from, target, s.Terrain.Blocks)
			return clear, nil
		}})

	r.Add(Op{Module: "tabletop", Name: "tag_terrain", MCPTool: "tabletop_tag_terrain",
		Call: func(state any, args map[string]any) (any, error) {
			s, err := asTabletop(state)
			if err != nil {
				return nil, err
			}
			p, err := pos(args, "x", "y")
			if err != nil {
				return nil, err
			}
			tag, err := argStr(args, "tag")
			if err != nil {
				return nil, err
			}
			s.Terrain.Tag(p, tag)
			return nil, nil
		}})

	r.Add(Op{Module: "tabletop", Name: "has_terrain", MCPTool: "tabletop_has_terrain",
		Call: func(state any, args map[string]any) (any, error) {
			s, err := asTabletop(state)
			if err != nil {
				return nil, err
			}
			p, err := pos(args, "x", "y")
			if err != nil {
				return nil, err
			}
			tag, err := argStr(args, "tag")
			if err != nil {
				return nil, err
			}
			return s.Terrain.HasTag(p, tag), nil
		}})
```

- [ ] **Step 4: Run to confirm pass**

Run: `export PATH=$HOME/.local/go/bin:$PATH && go test ./modulebridge/ -run TestTabletop -v`
Expected: PASS (all tabletop tests).

- [ ] **Step 5: Full modulebridge suite**

Run: `export PATH=$HOME/.local/go/bin:$PATH && go test ./modulebridge/`
Expected: ok (ccg tests still green too).

- [ ] **Step 6: Commit**

```bash
git add modulebridge/tabletop.go modulebridge/tabletop_test.go
git commit -m "modulebridge: tabletop spatial/geometry/terrain/LOS op set"
```

---

## Task 3: Starlark integration + replay determinism

**Files:**
- Create: `mcp/starlarkgame/modules_tabletop_test.go`

Prove a tabletop-backed spec runs through the real `match.Manager` + in-memory store, that positions persist across moves, and that `core.Replay` reconstructs byte-identical state. Mirrors the phase-1 `modules_replay_test.go` (read it for the exact `core.Replay` signature: `core.Replay(g *core.Game, log []core.LogEntry, numPlayers int, setupData any) (core.State, error)`, and `JoinResult.PlayerCredentials`).

- [ ] **Step 1: Write `mcp/starlarkgame/modules_tabletop_test.go`**

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

// A 1-player hex-skirmish spec: setup makes a 6x6 hex board and places
// unit 1 at (0,0). The "advance" move steps unit 1 to the first neighbor.
const miniTabletopSpec = `
META = {"name": "mini-tabletop", "min_players": 1, "max_players": 1}
MODULES = ["tabletop"]

def setup(ctx):
    ctx.modules.tabletop.new_board(kind="hex", w=6, h=6)
    ctx.modules.tabletop.place(unit=1, x=0, y=0)
    return {"steps": 0}

def end_if(state, ctx): return None
def legal_moves(state, ctx): return [{"name": "advance", "args": []}]

def advance(state, ctx):
    here = ctx.modules.tabletop.position_of(unit=1)
    ns = ctx.modules.tabletop.neighbors(x=here[0], y=here[1])
    dest = ns[0]
    ctx.modules.tabletop.move(unit=1, x=dest[0], y=dest[1])
    return {"steps": state["steps"] + 1}

MOVES = {"advance": {"apply": advance}}
`

func TestMiniTabletop_SetupRunReplay(t *testing.T) {
	spec, err := LoadSpec(miniTabletopSpec)
	if err != nil {
		t.Fatalf("LoadSpec: %v", err)
	}
	if err := Validate(context.Background(), spec); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	g := BuildCoreGame(spec)

	mgr := match.NewManager(storage.NewMemory())
	mgr.MustRegister(g)
	id, err := mgr.Create("mini-tabletop", match.CreateOptions{NumPlayers: 1})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	jr, err := mgr.Join(id, "p", match.JoinOptions{})
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	for i := 0; i < 2; i++ {
		if _, err := mgr.MoveReq(id, jr.PlayerID, jr.PlayerCredentials, core.MoveRequest{Move: "advance"}); err != nil {
			t.Fatalf("advance %d: %v", i, err)
		}
	}

	st, _ := mgr.State(id)
	sg := st.State.G.(*StarlarkG)
	if sg.Data["steps"] != int64(2) && sg.Data["steps"] != float64(2) {
		t.Fatalf("steps = %v, want 2", sg.Data["steps"])
	}

	replayed, err := core.Replay(g, st.State.Log, 1, nil)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	orig, _ := json.Marshal(st.State.G)
	repl, _ := json.Marshal(replayed.G)
	if string(orig) != string(repl) {
		t.Fatalf("replay mismatch:\n orig=%s\n repl=%s", orig, repl)
	}
}
```

NOTE on the `steps` assertion: the move's `apply` returns `{"steps": state["steps"] + 1}`. Whether `state["steps"]` arrives as `int64` or `float64` depends on the conv layer; the test accepts either. If the spec's first move fails because `position_of` returns something unexpected, debug methodically — `position_of` returns a 2-element list `[x, y]` (int64s), and `neighbors` returns a list of such lists, so `ns[0]` is `[x, y]` and `dest[0]`/`dest[1]` are ints.

- [ ] **Step 2: Run it**

Run: `export PATH=$HOME/.local/go/bin:$PATH && go test ./starlarkgame/ -run TestMiniTabletop -v`
Three outcomes, same handling as phase 1:
- **Compile error** on `core.Replay`/`match`/`storage` API: confirm the real signatures (`grep -n "func Replay" ../core/replay.go`) and adjust. They match phase 1's already-working `modules_replay_test.go` in this same package — cross-check that file.
- **PASS:** proceed to commit.
- **Assertion failure** (steps wrong, or replay mismatch): a real bug. Do NOT weaken the test. Replay re-runs setup (which calls `new_board` + `place`), so the board and positions rebuild deterministically; a mismatch likely means module state isn't threaded through the move closure (should already work from phase 1). Diagnose, don't hack.

- [ ] **Step 3: Full starlarkgame + modulebridge suites**

Run: `export PATH=$HOME/.local/go/bin:$PATH && go test ./starlarkgame/ ./modulebridge/`
Expected: ok / ok (phase-1 ccg tests stay green).

- [ ] **Step 4: Commit**

```bash
git add starlarkgame/modules_tabletop_test.go
git commit -m "starlarkgame: tabletop integration + replay determinism test"
```

---

## Task 4: Documentation

**Files:**
- Modify: `mcp/docs/designed-games.md`

- [ ] **Step 1: Read the existing "Using engine modules (MODULES)" section** (added in phase 1) so the new content matches its style and placement.

- [ ] **Step 2: Add a `tabletop` subsection** right after the ccg example. Use this content:

```markdown
### tabletop module (spatial games)

For board/grid games, declare `tabletop` and build a board in setup, then place
and move units by integer id on integer `(x, y)` coordinates:

    MODULES = ["tabletop"]

    def setup(ctx):
        ctx.modules.tabletop.new_board(kind="hex", w=10, h=10)  # or kind="square"
        ctx.modules.tabletop.place(unit=1, x=0, y=0)
        return {}

    def advance(state, ctx):
        x, y = ctx.modules.tabletop.position_of(unit=1)
        ns = ctx.modules.tabletop.neighbors(x=x, y=y)
        ctx.modules.tabletop.move(unit=1, x=ns[0][0], y=ns[0][1])
        return state

Ops: `new_board(kind, w, h)`, `place(unit, x, y)`, `move(unit, x, y)`,
`remove(unit)`, `position_of(unit)` → `[x, y]` or `None`, `entities_at(x, y)` →
`[unit, …]`, `within(x, y, radius)` → `[unit, …]`, `neighbors(x, y)` →
`[[x, y], …]`, `line_of_sight(fx, fy, tx, ty)` → bool (honors terrain tagged
`"blocks_los"`), `tag_terrain(x, y, tag)`, `has_terrain(x, y, tag)`. Units are
plain integer ids you choose; positions are plain coordinates. Dice/combat are
not yet bridged.
```

- [ ] **Step 3: Build everything + commit**

Run: `export PATH=$HOME/.local/go/bin:$PATH && go build ./... && go test ./starlarkgame/ ./modulebridge/`
Expected: clean build + green.

```bash
git add docs/designed-games.md
git commit -m "docs: document the tabletop module for designed games"
```

---

## Self-Review

**Spec coverage:** Phase-2 scope from the spec ("tabletop binding: hex/square/terrain/combat") is delivered for hex/square geometry, positions, terrain, and LOS. Combat/dice is explicitly deferred (RNG-determinism wiring is its own follow-up) and called out in scope notes + docs. ✓

**Reuse validation:** Tasks touch only `modulebridge/tabletop.go`, one whitelist line in `spec.go`, tests, and docs — confirming the phase-1 `ctx.modules` wiring, Setup instantiation, validate scan, and `module_op` tool are all generic. If any of those needed editing for tabletop, that is a signal the phase-1 abstraction leaked — investigate rather than patching around it. ✓

**Placeholder scan:** No TBD/TODO. The debug guidance in Task 3 Step 2 is conditional handling with concrete diagnosis steps, not a placeholder. ✓

**Type consistency:** `tabletopState{Space, Board, Terrain}`, `newTabletopState`, `TabletopRegistry`, `asTabletop`, `pos`/`posList` helpers, and the `argStr/argInt/argBool` reuse from `ccg.go` are consistent across tasks. Op names match between registration, tests, and docs (`new_board`, `place`, `move`, `remove`, `position_of`, `entities_at`, `within`, `neighbors`, `line_of_sight`, `tag_terrain`, `has_terrain`).

**Determinism:** Every op is a pure function of (state, args) — no RNG, no wall-clock. `EntitiesAt`/`Within` sort, `Neighbors`/`Line` are deterministic. Replay safety holds (Task 3 proves it). ✓

**Known caveat (carry forward):** `tabletopState.Board` is an interface; JSON marshals concrete fields without a type tag, so a serializing store could not faithfully reload the board type. Consistent with the phase-1 in-memory caveat (modules aren't round-tripped between moves under the memory store; replay rebuilds via setup). Out of scope to fix here.
