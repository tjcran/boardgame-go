# Engine Modules in Starlark — Phase 1 (Core Bridge + ccg) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let Starlark-authored games use the engine's `modules/ccg` package (zones, entities, decks, hands) through `ctx.modules.ccg.*` bindings, with the same operations also exposed as design-time MCP tools, proving the whole bridge mechanism on one module.

**Architecture:** Designed games get a real struct `G` (`StarlarkG{Data, Modules}`) instead of a bare `map[string]any`, so live module states (`*ccg.State`) with their unexported ID counters persist in memory across moves exactly like a native Go game. A new `mcp/modulebridge/` package holds one Go function per engine op in a shared `Registry`, wrapped twice: as a `ctx.modules.<name>.<op>()` Starlark builtin (runtime) and as an MCP tool (design-time). An opaque-handle layer maps `ccg.EntityID` to stable string tokens (`"ent:7"`) across the boundary.

**Tech Stack:** Go, `go.starlark.net`, `modules/ccg`, the existing `mcp/starlarkgame` adapter, `match.Manager` with the in-memory store.

**Reference spec:** `docs/superpowers/specs/2026-05-21-engine-modules-in-starlark-and-mcp.md`

**Scope note:** This is phase 1 of 5 (see spec "Implementation phasing"). tabletop, economy/shop, TargetRequest, and HOOKS are later plans that reuse this mechanism. Do not build them here.

---

## File Structure

- `mcp/starlarkgame/g.go` (new) — `StarlarkG` struct + its `MarshalJSON`/`UnmarshalJSON`. Isolated from `game.go` so the type contract is in one focused file.
- `mcp/starlarkgame/game.go` (modify) — adapter closures switch from `map[string]any` G to `*StarlarkG`; `Setup` instantiates declared modules.
- `mcp/starlarkgame/spec.go` (modify) — parse top-level `MODULES` list into `Spec.Modules`.
- `mcp/starlarkgame/bridge.go` (modify) — `BridgeCtx` carries module states; `ctx.modules` sub-struct built from the registry.
- `mcp/modulebridge/registry.go` (new) — `Op`, `Registry`, module-state factories.
- `mcp/modulebridge/handles.go` (new) — `EntityID` ↔ token derivation.
- `mcp/modulebridge/ccg.go` (new) — the ccg op set (zones, entities, move/draw/etc.).
- `mcp/modulebridge/*_test.go` (new) — registry, handle, and per-op unit tests.
- `mcp/starlarkgame/modules_ccg_test.go` (new) — Starlark-level integration: a mini-CCG spec runs end to end.
- `mcp/starlarkgame/modules_replay_test.go` (new) — determinism/replay round-trip.
- `mcp/tools.go` (modify) — register ccg ops as design-time MCP tools from the registry.
- `mcp/tools_modules_test.go` (new) — dual-surface parity test.

---

## Task 1: Introduce `StarlarkG` and migrate the adapter

**Files:**
- Create: `mcp/starlarkgame/g.go`
- Modify: `mcp/starlarkgame/game.go` (closures at lines 32-91, 145-173)
- Test: `mcp/starlarkgame/g_test.go`

The adapter currently uses `map[string]any` as the game `G`. We replace it with a struct that also holds live module states, while keeping the JSON wire shape identical for module-free games (so existing games and clients are unaffected): `MarshalJSON` emits the bare `Data` dict, plus a reserved `__modules__` key only when modules are present.

- [ ] **Step 1: Write the failing test**

```go
// mcp/starlarkgame/g_test.go
package starlarkgame

import (
	"encoding/json"
	"testing"
)

func TestStarlarkG_MarshalsAsBareDataWhenNoModules(t *testing.T) {
	g := &StarlarkG{Data: map[string]any{"score": int64(3)}}
	b, err := json.Marshal(g)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// Module-free games must keep the legacy wire shape: G is the data dict.
	if string(b) != `{"score":3}` {
		t.Fatalf("got %s, want {\"score\":3}", b)
	}
}

func TestStarlarkG_RoundTripWithModulesKey(t *testing.T) {
	g := &StarlarkG{
		Data:    map[string]any{"score": int64(3)},
		Modules: map[string]any{"ccg": map[string]any{"zones": map[string]any{}}},
	}
	b, err := json.Marshal(g)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back StarlarkG
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.Data["score"] != float64(3) { // JSON numbers decode to float64
		t.Fatalf("data lost: %#v", back.Data)
	}
	if _, ok := back.Modules["ccg"]; !ok {
		t.Fatalf("modules key lost: %#v", back.Modules)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd mcp && go test ./starlarkgame/ -run TestStarlarkG -v`
Expected: FAIL — `undefined: StarlarkG`.

- [ ] **Step 3: Write the `StarlarkG` type**

```go
// mcp/starlarkgame/g.go
package starlarkgame

import "encoding/json"

// StarlarkG is the game state (core.G) for designed games. Data is the
// user dict the spec's apply reads and returns. Modules holds live
// engine-module states ("ccg" -> *ccg.State, …) instantiated in Setup
// and carried in memory across moves, so their unexported ID counters
// persist exactly like a native Go game's G under the in-memory store.
//
// JSON contract: a module-free game marshals as the bare Data dict, so
// existing games and clients see the unchanged wire shape. When modules
// are present they are nested under the reserved "__modules__" key.
type StarlarkG struct {
	Data    map[string]any
	Modules map[string]any
}

const modulesKey = "__modules__"

func (g *StarlarkG) MarshalJSON() ([]byte, error) {
	if len(g.Modules) == 0 {
		if g.Data == nil {
			return []byte("{}"), nil
		}
		return json.Marshal(g.Data)
	}
	out := make(map[string]any, len(g.Data)+1)
	for k, v := range g.Data {
		out[k] = v
	}
	out[modulesKey] = g.Modules
	return json.Marshal(out)
}

func (g *StarlarkG) UnmarshalJSON(b []byte) error {
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	if mods, ok := raw[modulesKey]; ok {
		if m, ok := mods.(map[string]any); ok {
			g.Modules = m
		}
		delete(raw, modulesKey)
	}
	g.Data = raw
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd mcp && go test ./starlarkgame/ -run TestStarlarkG -v`
Expected: PASS.

- [ ] **Step 5: Migrate the adapter closures in `game.go`**

In `BuildCoreGame`, change every `mc.G.(map[string]any)` / `gv.(map[string]any)` to read `*StarlarkG`, operate on `.Data`, and return a `*StarlarkG`. Replace the five sites:

```go
// game.go — Setup
g.Setup = func(ctx core.Ctx, _ any) core.G {
	bc := &BridgeCtx{NumPlayers: ctx.NumPlayers}
	bc.AttachSeededRandom(ctxSeed(ctx))
	data, err := s.CallSetup(context.Background(), bc)
	if err != nil {
		return &StarlarkG{Data: map[string]any{"__starlark_setup_error__": err.Error()}}
	}
	return &StarlarkG{Data: data, Modules: map[string]any{}}
}

// game.go — EndIf
g.EndIf = func(mc *core.MoveContext) any {
	bc := &BridgeCtx{NumPlayers: mc.Ctx.NumPlayers}
	bc.AttachSeededRandom(ctxSeed(mc.Ctx))
	sg, ok := mc.G.(*StarlarkG)
	if !ok {
		return nil
	}
	out, err := s.CallEndIf(context.Background(), bc, sg.Data)
	if err != nil {
		return nil
	}
	return out
}

// game.go — PlayerView (inside the `if s.PlayerView != nil` block)
g.PlayerView = func(gv core.G, ctx core.Ctx, playerID string) core.G {
	bc := &BridgeCtx{NumPlayers: ctx.NumPlayers, PlayerID: playerID}
	sg, ok := gv.(*StarlarkG)
	if !ok {
		return gv
	}
	out, err := s.CallPlayerView(context.Background(), bc, sg.Data, playerID)
	if err != nil {
		return gv
	}
	return &StarlarkG{Data: out, Modules: sg.Modules}
}

// game.go — Enumerate
g.Enumerate = func(gv core.G, ctx core.Ctx, playerID string) []core.EnumerateAction {
	bc := &BridgeCtx{NumPlayers: ctx.NumPlayers, PlayerID: playerID}
	bc.AttachSeededRandom(ctxSeed(ctx))
	sg, ok := gv.(*StarlarkG)
	if !ok {
		return nil
	}
	out, err := s.CallLegalMoves(context.Background(), bc, sg.Data)
	// … unchanged body using `out` …
}
```

And in `buildMovesMap`:

```go
out[name] = core.MoveFn(func(mc *core.MoveContext, args ...any) (core.G, error) {
	sg, ok := mc.G.(*StarlarkG)
	if !ok {
		return nil, fmt.Errorf("starlarkgame: state is not *StarlarkG")
	}
	bc := &BridgeCtx{
		NumPlayers: mc.Ctx.NumPlayers,
		PlayerID:   mc.PlayerID,
		Phase:      mc.Ctx.Phase,
		Events:     mc.Events,
		Modules:    sg.Modules, // wired in Task 4
	}
	bc.AttachSeededRandom(ctxSeed(mc.Ctx))
	newData, err := s.CallMove(context.Background(), bc, name, sg.Data, args)
	if err != nil {
		return nil, err
	}
	if endsTurn && mc.Events != nil {
		mc.Events.EndTurn()
	}
	return &StarlarkG{Data: newData, Modules: sg.Modules}, nil
})
```

Add the `Modules map[string]any` field to `BridgeCtx` in `bridge.go` now (used in Task 4) so this compiles:

```go
// bridge.go — add to BridgeCtx
	// Modules holds live engine-module states for ctx.modules.* bindings.
	// Keyed by module name ("ccg"). Nil when the spec declares no MODULES.
	Modules map[string]any
```

- [ ] **Step 6: Run the full starlarkgame suite to verify no regression**

Run: `cd mcp && go test ./starlarkgame/ -v`
Expected: PASS — every existing e2e/phase/stage test still green (module-free games are unaffected because `MarshalJSON` preserves the bare-dict shape).

- [ ] **Step 7: Commit**

```bash
git add mcp/starlarkgame/g.go mcp/starlarkgame/g_test.go mcp/starlarkgame/game.go mcp/starlarkgame/bridge.go
git commit -m "starlarkgame: introduce StarlarkG struct G to carry live module state"
```

---

## Task 2: Parse the `MODULES` declaration

**Files:**
- Modify: `mcp/starlarkgame/spec.go` (add `Modules` field to `Spec` ~line 70; parse after `readStages` ~line 182)
- Test: `mcp/starlarkgame/spec_test.go`

Specs declare which modules they use: `MODULES = ["ccg"]`. Only known module names are accepted; unknown names fail at load time.

- [ ] **Step 1: Write the failing test**

```go
// append to mcp/starlarkgame/spec_test.go
func TestLoadSpec_ParsesModules(t *testing.T) {
	src := `
META = {"name": "m", "min_players": 2, "max_players": 2}
MODULES = ["ccg"]
def setup(ctx): return {}
def end_if(state, ctx): return None
def legal_moves(state, ctx): return []
MOVES = {"pass": {"apply": lambda state, ctx: state}}
`
	s, err := LoadSpec(src)
	if err != nil {
		t.Fatalf("LoadSpec: %v", err)
	}
	if len(s.Modules) != 1 || s.Modules[0] != "ccg" {
		t.Fatalf("got modules %v, want [ccg]", s.Modules)
	}
}

func TestLoadSpec_RejectsUnknownModule(t *testing.T) {
	src := `
META = {"name": "m", "min_players": 2, "max_players": 2}
MODULES = ["nope"]
def setup(ctx): return {}
def end_if(state, ctx): return None
def legal_moves(state, ctx): return []
MOVES = {"pass": {"apply": lambda state, ctx: state}}
`
	if _, err := LoadSpec(src); err == nil {
		t.Fatal("expected error for unknown module")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd mcp && go test ./starlarkgame/ -run TestLoadSpec_ -v`
Expected: FAIL — `s.Modules` undefined.

- [ ] **Step 3: Add field + parser**

Add to the `Spec` struct (after `Stages`):

```go
	// Modules is the optional top-level MODULES list naming engine
	// modules the spec uses via ctx.modules.<name>.*. Empty when absent.
	Modules []string
```

Add to `LoadSpec`, immediately before `return s, nil`:

```go
	if err := readModules(globals, s); err != nil {
		return nil, err
	}
```

Add the parser function (knownModules is the whitelist; phase 1 ships only "ccg"):

```go
var knownModules = map[string]bool{"ccg": true}

func readModules(globals starlark.StringDict, s *Spec) error {
	raw, ok := globals["MODULES"]
	if !ok {
		return nil
	}
	lst, ok := raw.(*starlark.List)
	if !ok {
		return fmt.Errorf("MODULES must be a list, got %s", raw.Type())
	}
	iter := lst.Iterate()
	defer iter.Done()
	var x starlark.Value
	for iter.Next(&x) {
		name, ok := x.(starlark.String)
		if !ok {
			return fmt.Errorf("MODULES entry %v must be a string", x)
		}
		if !knownModules[string(name)] {
			return fmt.Errorf("MODULES: unknown module %q", string(name))
		}
		s.Modules = append(s.Modules, string(name))
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd mcp && go test ./starlarkgame/ -run TestLoadSpec_ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add mcp/starlarkgame/spec.go mcp/starlarkgame/spec_test.go
git commit -m "starlarkgame: parse MODULES declaration with a known-name whitelist"
```

---

## Task 3: `modulebridge` registry + handle table

**Files:**
- Create: `mcp/modulebridge/registry.go`
- Create: `mcp/modulebridge/handles.go`
- Test: `mcp/modulebridge/handles_test.go`

The registry is the single source of truth both surfaces iterate. The handle table converts `ccg.EntityID` to/from stable `"ent:<n>"` tokens so neither Starlark nor MCP ever sees a raw Go type. Tokens derive deterministically from the ID (no allocation counter), so the mapping is a pure function of state.

- [ ] **Step 1: Write the failing test**

```go
// mcp/modulebridge/handles_test.go
package modulebridge

import (
	"testing"

	"github.com/tjcran/boardgame-go/modules/ccg"
)

func TestEntityToken_RoundTrips(t *testing.T) {
	tok := EntityToken(ccg.EntityID(7))
	if tok != "ent:7" {
		t.Fatalf("got %q, want ent:7", tok)
	}
	id, err := ParseEntityToken("ent:7")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if id != ccg.EntityID(7) {
		t.Fatalf("got %d, want 7", id)
	}
}

func TestParseEntityToken_RejectsGarbage(t *testing.T) {
	for _, bad := range []string{"7", "ent:", "ent:x", "hex:1,2", ""} {
		if _, err := ParseEntityToken(bad); err == nil {
			t.Fatalf("expected error for %q", bad)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd mcp && go test ./modulebridge/ -v`
Expected: FAIL — package/symbols undefined.

- [ ] **Step 3: Write handles.go and registry.go**

```go
// mcp/modulebridge/handles.go
package modulebridge

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/tjcran/boardgame-go/modules/ccg"
)

// ErrBadHandle is returned when a token does not match its expected
// shape. Surfaced verbatim to Starlark (move rejected) or MCP callers.
var ErrBadHandle = errors.New("modulebridge: bad handle token")

// EntityToken renders a ccg.EntityID as the stable token "ent:<n>".
func EntityToken(id ccg.EntityID) string {
	return "ent:" + strconv.FormatUint(uint64(id), 10)
}

// ParseEntityToken is the inverse of EntityToken.
func ParseEntityToken(tok string) (ccg.EntityID, error) {
	rest, ok := strings.CutPrefix(tok, "ent:")
	if !ok || rest == "" {
		return 0, fmt.Errorf("%w: %q (want ent:<n>)", ErrBadHandle, tok)
	}
	n, err := strconv.ParseUint(rest, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: %q", ErrBadHandle, tok)
	}
	return ccg.EntityID(n), nil
}
```

```go
// mcp/modulebridge/registry.go
package modulebridge

// Op is one engine operation, exposed on both surfaces (Starlark
// ctx.modules.<Module>.<starlark name> and the MCP tool <MCPTool>).
//
// Call decodes args (already converted to plain Go values) against a
// live module state and returns a plain Go result. Determinism: an Op
// must be a pure function of (state, args, seeded RNG). Ops that read
// wall-clock or unseeded randomness must not be registered.
type Op struct {
	Module  string                              // "ccg"
	Name    string                              // starlark/MCP method name, e.g. "move_to"
	MCPTool string                              // MCP tool name, e.g. "ccg_move_to"
	Call    func(state any, args map[string]any) (any, error)
}

// Registry is the immutable set of ops for a module, built once at init.
type Registry struct {
	ops map[string][]Op // module -> ops
}

func NewRegistry() *Registry { return &Registry{ops: map[string][]Op{}} }

func (r *Registry) Add(op Op) { r.ops[op.Module] = append(r.ops[op.Module], op) }

// Ops returns the ops registered for a module (nil if none).
func (r *Registry) Ops(module string) []Op { return r.ops[module] }

// Modules returns every module name with at least one op.
func (r *Registry) Modules() []string {
	out := make([]string, 0, len(r.ops))
	for m := range r.ops {
		out = append(out, m)
	}
	return out
}

// NewState mints a fresh live state for a module by name. Returns nil
// for unknown modules. Used by Setup to populate StarlarkG.Modules.
func NewState(module string) any {
	if f := stateFactories[module]; f != nil {
		return f()
	}
	return nil
}

// stateFactories maps module name -> constructor. Populated by each
// module's binding file (e.g. ccg.go's init).
var stateFactories = map[string]func() any{}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd mcp && go test ./modulebridge/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add mcp/modulebridge/handles.go mcp/modulebridge/registry.go mcp/modulebridge/handles_test.go
git commit -m "modulebridge: registry skeleton + ccg entity handle tokens"
```

---

## Task 4: ccg op set + state factory

**Files:**
- Create: `mcp/modulebridge/ccg.go`
- Test: `mcp/modulebridge/ccg_test.go`

Register the phase-1 ccg ops against the live `*ccg.State`. Each op takes a `state any` (asserted to `*ccg.State`) and a `map[string]any` of named args, returning plain Go values (tokens for entities). This keeps the registry surface uniform for both the Starlark and MCP wrappers.

- [ ] **Step 1: Write the failing test**

```go
// mcp/modulebridge/ccg_test.go
package modulebridge

import (
	"testing"

	"github.com/tjcran/boardgame-go/modules/ccg"
)

func ccgOp(t *testing.T, name string) Op {
	t.Helper()
	for _, op := range CCGRegistry().Ops("ccg") {
		if op.Name == name {
			return op
		}
	}
	t.Fatalf("op %q not registered", name)
	return Op{}
}

func TestCCG_NewZoneNewEntityMoveTo(t *testing.T) {
	st := ccg.NewState()

	if _, err := ccgOp(t, "new_zone").Call(st, map[string]any{"name": "hand", "ordered": false}); err != nil {
		t.Fatalf("new_zone: %v", err)
	}
	res, err := ccgOp(t, "new_entity").Call(st, map[string]any{"type": "card", "owner": "0"})
	if err != nil {
		t.Fatalf("new_entity: %v", err)
	}
	tok, _ := res.(string)
	if tok != "ent:1" {
		t.Fatalf("got token %q, want ent:1", tok)
	}
	if _, err := ccgOp(t, "move_to").Call(st, map[string]any{"entity": tok, "zone": "hand"}); err != nil {
		t.Fatalf("move_to: %v", err)
	}
	sizeRes, err := ccgOp(t, "size").Call(st, map[string]any{"zone": "hand"})
	if err != nil {
		t.Fatalf("size: %v", err)
	}
	if sizeRes.(int64) != 1 {
		t.Fatalf("got size %v, want 1", sizeRes)
	}
}

func TestCCG_MoveTo_BadHandle(t *testing.T) {
	st := ccg.NewState()
	st.NewZone("hand", false)
	_, err := ccgOp(t, "move_to").Call(st, map[string]any{"entity": "ent:99", "zone": "hand"})
	if err == nil {
		t.Fatal("expected ErrUnknownEntity for missing entity")
	}
}

func TestCCG_StateFactory(t *testing.T) {
	if _, ok := NewState("ccg").(*ccg.State); !ok {
		t.Fatal("NewState(ccg) did not return *ccg.State")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd mcp && go test ./modulebridge/ -run TestCCG -v`
Expected: FAIL — `CCGRegistry` undefined.

- [ ] **Step 3: Write ccg.go**

```go
// mcp/modulebridge/ccg.go
package modulebridge

import (
	"fmt"

	"github.com/tjcran/boardgame-go/modules/ccg"
)

func init() {
	stateFactories["ccg"] = func() any { return ccg.NewState() }
}

var ccgReg *Registry

// CCGRegistry returns the registry holding the ccg op set, built once.
func CCGRegistry() *Registry {
	if ccgReg == nil {
		ccgReg = buildCCGRegistry()
	}
	return ccgReg
}

func asCCG(state any) (*ccg.State, error) {
	s, ok := state.(*ccg.State)
	if !ok {
		return nil, fmt.Errorf("modulebridge: ccg op got %T, want *ccg.State", state)
	}
	return s, nil
}

func argStr(args map[string]any, key string) (string, error) {
	v, ok := args[key]
	if !ok {
		return "", fmt.Errorf("missing arg %q", key)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("arg %q must be string, got %T", key, v)
	}
	return s, nil
}

func argBool(args map[string]any, key string) bool {
	b, _ := args[key].(bool)
	return b
}

func argInt(args map[string]any, key string) (int, error) {
	switch v := args[key].(type) {
	case int64:
		return int(v), nil
	case int:
		return v, nil
	case float64:
		return int(v), nil
	default:
		return 0, fmt.Errorf("arg %q must be int, got %T", key, args[key])
	}
}

func buildCCGRegistry() *Registry {
	r := NewRegistry()

	r.Add(Op{Module: "ccg", Name: "new_zone", MCPTool: "ccg_new_zone",
		Call: func(state any, args map[string]any) (any, error) {
			s, err := asCCG(state)
			if err != nil {
				return nil, err
			}
			name, err := argStr(args, "name")
			if err != nil {
				return nil, err
			}
			s.NewZone(ccg.ZoneName(name), argBool(args, "ordered"))
			return nil, nil
		}})

	r.Add(Op{Module: "ccg", Name: "new_entity", MCPTool: "ccg_new_entity",
		Call: func(state any, args map[string]any) (any, error) {
			s, err := asCCG(state)
			if err != nil {
				return nil, err
			}
			typ, err := argStr(args, "type")
			if err != nil {
				return nil, err
			}
			owner, _ := args["owner"].(string)
			var attrs map[string]any
			if a, ok := args["attrs"].(map[string]any); ok {
				attrs = a
			}
			id := s.NewEntity(typ, owner, attrs)
			return EntityToken(id), nil
		}})

	r.Add(Op{Module: "ccg", Name: "move_to", MCPTool: "ccg_move_to",
		Call: func(state any, args map[string]any) (any, error) {
			s, err := asCCG(state)
			if err != nil {
				return nil, err
			}
			tok, err := argStr(args, "entity")
			if err != nil {
				return nil, err
			}
			id, err := ParseEntityToken(tok)
			if err != nil {
				return nil, err
			}
			zone, err := argStr(args, "zone")
			if err != nil {
				return nil, err
			}
			return nil, s.MoveTo(id, ccg.ZoneName(zone))
		}})

	r.Add(Op{Module: "ccg", Name: "size", MCPTool: "ccg_size",
		Call: func(state any, args map[string]any) (any, error) {
			s, err := asCCG(state)
			if err != nil {
				return nil, err
			}
			zone, err := argStr(args, "zone")
			if err != nil {
				return nil, err
			}
			return int64(s.Size(ccg.ZoneName(zone))), nil
		}})

	r.Add(Op{Module: "ccg", Name: "members", MCPTool: "ccg_members",
		Call: func(state any, args map[string]any) (any, error) {
			s, err := asCCG(state)
			if err != nil {
				return nil, err
			}
			zone, err := argStr(args, "zone")
			if err != nil {
				return nil, err
			}
			z, ok := s.Zones[ccg.ZoneName(zone)]
			if !ok {
				return nil, ccg.ErrUnknownZone
			}
			out := make([]any, 0, len(z.Members))
			for _, m := range z.Members {
				out = append(out, EntityToken(m))
			}
			return out, nil
		}})

	r.Add(Op{Module: "ccg", Name: "draw", MCPTool: "ccg_draw",
		Call: func(state any, args map[string]any) (any, error) {
			s, err := asCCG(state)
			if err != nil {
				return nil, err
			}
			zone, err := argStr(args, "zone")
			if err != nil {
				return nil, err
			}
			n, err := argInt(args, "n")
			if err != nil {
				return nil, err
			}
			drawn, err := s.Draw(ccg.ZoneName(zone), n)
			if err != nil {
				return nil, err
			}
			out := make([]any, 0, len(drawn))
			for _, id := range drawn {
				out = append(out, EntityToken(id))
			}
			return out, nil
		}})

	return r
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd mcp && go test ./modulebridge/ -run TestCCG -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add mcp/modulebridge/ccg.go mcp/modulebridge/ccg_test.go
git commit -m "modulebridge: ccg op set (zones, entities, move/draw) + state factory"
```

---

## Task 5: Instantiate modules in Setup + wire `ctx.modules`

**Files:**
- Modify: `mcp/starlarkgame/game.go` (Setup closure)
- Modify: `mcp/starlarkgame/bridge.go` (`asStarlark` adds `ctx.modules`; new `modulesAsStarlark`)
- Modify: `mcp/starlarkgame/spec.go` (pass `s.Modules` into Setup wiring — via closure capture)
- Test: `mcp/starlarkgame/modules_ccg_test.go`

`Setup` creates a live state per declared module into `StarlarkG.Modules`. `ctx.modules.<name>.<op>(**kwargs)` builtins call the registry op against the live state. Args are passed as Starlark **kwargs** (named), converted to `map[string]any` via the existing `ToGo`.

- [ ] **Step 1: Write the failing integration test**

```go
// mcp/starlarkgame/modules_ccg_test.go
package starlarkgame

import (
	"context"
	"testing"
)

// A minimal ccg-backed spec: setup builds a deck of 3 cards and a hand;
// the "draw" move moves the top deck card into the hand.
const miniCCGSpec = `
META = {"name": "mini-ccg", "min_players": 1, "max_players": 1}
MODULES = ["ccg"]

def setup(ctx):
    ctx.modules.ccg.new_zone(name="deck", ordered=True)
    ctx.modules.ccg.new_zone(name="hand", ordered=False)
    for i in range(3):
        e = ctx.modules.ccg.new_entity(type="card", owner="0")
        ctx.modules.ccg.move_to(entity=e, zone="deck")
    return {}

def end_if(state, ctx): return None
def legal_moves(state, ctx): return [{"name": "draw", "args": []}]

def draw(state, ctx):
    drawn = ctx.modules.ccg.draw(zone="deck", n=1)
    ctx.modules.ccg.move_to(entity=drawn[0], zone="hand")
    return state

MOVES = {"draw": {"apply": draw}}
`

func TestMiniCCG_SetupAndDraw(t *testing.T) {
	spec, err := LoadSpec(miniCCGSpec)
	if err != nil {
		t.Fatalf("LoadSpec: %v", err)
	}
	if err := Validate(context.Background(), spec); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	g := BuildCoreGame(spec)

	sg := g.Setup(coreCtx(1), nil).(*StarlarkG)
	st := ccgStateOf(t, sg) // helper asserts sg.Modules["ccg"] is *ccg.State
	if st.Size("deck") != 3 || st.Size("hand") != 0 {
		t.Fatalf("after setup: deck=%d hand=%d, want 3/0", st.Size("deck"), st.Size("hand"))
	}
}
```

Add the two small helpers (`coreCtx`, `ccgStateOf`) at the bottom of the test file:

```go
import (
	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/modules/ccg"
)

func coreCtx(n int) core.Ctx { return core.Ctx{NumPlayers: n} }

func ccgStateOf(t *testing.T, sg *StarlarkG) *ccg.State {
	t.Helper()
	st, ok := sg.Modules["ccg"].(*ccg.State)
	if !ok {
		t.Fatalf("modules[ccg] is %T, want *ccg.State", sg.Modules["ccg"])
	}
	return st
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd mcp && go test ./starlarkgame/ -run TestMiniCCG -v`
Expected: FAIL — `ctx.modules` has no attribute, or Modules not populated.

- [ ] **Step 3: Populate Modules in Setup**

Replace the Setup closure body in `game.go` so it instantiates declared modules **before** calling the spec's setup, and threads them through `BridgeCtx`:

```go
g.Setup = func(ctx core.Ctx, _ any) core.G {
	mods := map[string]any{}
	for _, name := range s.Modules {
		if st := modulebridge.NewState(name); st != nil {
			mods[name] = st
		}
	}
	bc := &BridgeCtx{NumPlayers: ctx.NumPlayers, Modules: mods}
	bc.AttachSeededRandom(ctxSeed(ctx))
	data, err := s.CallSetup(context.Background(), bc)
	if err != nil {
		return &StarlarkG{Data: map[string]any{"__starlark_setup_error__": err.Error()}, Modules: mods}
	}
	return &StarlarkG{Data: data, Modules: mods}
}
```

Add the import `"github.com/tjcran/boardgame-go/mcp/modulebridge"` to `game.go`.

- [ ] **Step 4: Wire `ctx.modules` in bridge.go**

In `asStarlark`, after the `Events` block, add:

```go
	if len(c.Modules) > 0 {
		attrs["modules"] = c.modulesAsStarlark()
	}
```

Add the builder. It blank-imports nothing; it reads the union of registries for the modules present. For phase 1 only ccg exists, so we look its ops up via `modulebridge.CCGRegistry()`:

```go
// bridge.go
import (
	"github.com/tjcran/boardgame-go/mcp/modulebridge"
	// … existing imports …
)

// modulesAsStarlark builds the ctx.modules struct: one attribute per
// live module, each a struct of op builtins bound to that module's
// live state. Args are passed as Starlark keyword arguments.
func (c *BridgeCtx) modulesAsStarlark() starlark.Value {
	modAttrs := starlark.StringDict{}
	for name, state := range c.Modules {
		reg := modulebridge.RegistryFor(name)
		if reg == nil {
			continue
		}
		opAttrs := starlark.StringDict{}
		for _, op := range reg.Ops(name) {
			op := op
			st := state
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
					res, err := op.Call(st, m)
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
```

Add `RegistryFor` to `modulebridge/registry.go` so bridge.go does not hardcode ccg:

```go
// RegistryFor returns the op registry for a module name, or nil if the
// module is unknown. Each binding file registers itself here.
func RegistryFor(name string) *Registry { return registryByName[name] }

var registryByName = map[string]*Registry{}
```

And in `ccg.go`'s `init`, register it:

```go
func init() {
	stateFactories["ccg"] = func() any { return ccg.NewState() }
	registryByName["ccg"] = CCGRegistry()
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `cd mcp && go test ./starlarkgame/ -run TestMiniCCG -v`
Expected: PASS.

- [ ] **Step 6: Run the full starlarkgame + modulebridge suites**

Run: `cd mcp && go test ./starlarkgame/ ./modulebridge/ -v`
Expected: PASS (no regression in existing games).

- [ ] **Step 7: Commit**

```bash
git add mcp/starlarkgame/game.go mcp/starlarkgame/bridge.go mcp/starlarkgame/modules_ccg_test.go mcp/modulebridge/registry.go mcp/modulebridge/ccg.go
git commit -m "starlarkgame: instantiate modules in Setup and expose ctx.modules.ccg.*"
```

---

## Task 6: End-to-end move through the Manager + replay determinism

**Files:**
- Test: `mcp/starlarkgame/modules_replay_test.go`

Prove the live module state survives across moves under the real `match.Manager` + in-memory store, and that `core.Replay` reconstructs a byte-identical final state (the determinism guarantee).

- [ ] **Step 1: Write the failing test**

```go
// mcp/starlarkgame/modules_replay_test.go
package starlarkgame

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/match"
	"github.com/tjcran/boardgame-go/storage"
)

func TestMiniCCG_DrawAcrossMovesAndReplay(t *testing.T) {
	spec, err := LoadSpec(miniCCGSpec)
	if err != nil {
		t.Fatalf("LoadSpec: %v", err)
	}
	g := BuildCoreGame(spec)

	mgr := match.NewManager(storage.NewMemory())
	mgr.MustRegister(g)

	id, err := mgr.Create("mini-ccg", match.CreateOptions{NumPlayers: 1})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	jr, err := mgr.Join(id, "p", match.JoinOptions{})
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	// Two draws: deck should drop to 1, hand rise to 2 — proves the live
	// *ccg.State (and its unexported counters) persisted across moves.
	for i := 0; i < 2; i++ {
		if _, err := mgr.MoveReq(id, jr.PlayerID, jr.Credentials, core.MoveRequest{Move: "draw"}); err != nil {
			t.Fatalf("move %d: %v", i, err)
		}
	}

	st, _ := mgr.State(id)
	sg := st.State.G.(*StarlarkG)
	ccgState := ccgStateOf(t, sg)
	if ccgState.Size("deck") != 1 || ccgState.Size("hand") != 2 {
		t.Fatalf("after 2 draws: deck=%d hand=%d, want 1/2", ccgState.Size("deck"), ccgState.Size("hand"))
	}

	// Replay the recorded log and assert byte-identical final G.
	replayed := core.Replay(g, st.State.Log)
	origJSON, _ := json.Marshal(st.State.G)
	replayJSON, _ := json.Marshal(replayed.G)
	if string(origJSON) != string(replayJSON) {
		t.Fatalf("replay mismatch:\n orig=%s\n repl=%s", origJSON, replayJSON)
	}
}
```

- [ ] **Step 2: Run test to verify it fails or passes**

Run: `cd mcp && go test ./starlarkgame/ -run TestMiniCCG_DrawAcrossMovesAndReplay -v`
Expected: Initially may FAIL if `core.Replay`'s signature differs — confirm the real signature with `grep -n "func Replay" ../core/replay.go` and adjust the call (it returns `core.State`; the log field name may be `st.State.Log`). Fix the test to match the real API, then it should PASS, proving persistence + determinism.

- [ ] **Step 3: If it fails on a real bug, debug with systematic-debugging**

If deck/hand counts are wrong, the live state is not persisting — check that `buildMovesMap` returns `&StarlarkG{Modules: sg.Modules}` (same map, Task 1 Step 5) and that the in-memory store shares the G pointer (`storage/memory.go:cloneMatch`). Do not "fix" by re-serializing; the design depends on the shared pointer.

- [ ] **Step 4: Run to verify it passes**

Run: `cd mcp && go test ./starlarkgame/ -run TestMiniCCG_DrawAcrossMovesAndReplay -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add mcp/starlarkgame/modules_replay_test.go
git commit -m "starlarkgame: e2e ccg draw across moves + replay determinism test"
```

---

## Task 7: Design-time MCP tools from the same registry

**Files:**
- Modify: `mcp/tools.go` (add a module-tool dispatcher)
- Test: `mcp/tools_modules_test.go`

Expose the ccg ops as MCP tools so Claude can poke at a draft match's module state interactively. The tool dispatcher resolves the live `*ccg.State` from the named match's `StarlarkG`, then calls the **same** registry op the Starlark binding uses — guaranteeing parity.

- [ ] **Step 1: Write the failing parity test**

```go
// mcp/tools_modules_test.go
package mcp

import (
	"context"
	"testing"

	"github.com/tjcran/boardgame-go/match"
	"github.com/tjcran/boardgame-go/mcp/starlarkgame"
	"github.com/tjcran/boardgame-go/storage"
)

func TestModuleTool_ParityWithStarlark(t *testing.T) {
	spec, err := starlarkgame.LoadSpec(miniCCGSpecForTools)
	if err != nil {
		t.Fatalf("LoadSpec: %v", err)
	}
	g := starlarkgame.BuildCoreGame(spec)
	mgr := match.NewManager(storage.NewMemory())
	mgr.MustRegister(g)
	id, _ := mgr.Create("mini-ccg", match.CreateOptions{NumPlayers: 1})
	mgr.Join(id, "p", match.JoinOptions{})

	tools := &Tools{Manager: mgr}

	// Calling the design-time MCP op must mutate the same live state the
	// Starlark path would. Add one card to the deck, confirm size via the
	// size op.
	if _, err := tools.ModuleOp(context.Background(), ModuleOpArgs{
		MatchID: id, Module: "ccg", Op: "new_zone",
		Args: map[string]any{"name": "deck", "ordered": true},
	}); err != nil {
		t.Fatalf("new_zone tool: %v", err)
	}
	if _, err := tools.ModuleOp(context.Background(), ModuleOpArgs{
		MatchID: id, Module: "ccg", Op: "new_entity",
		Args: map[string]any{"type": "card", "owner": "0"},
	}); err != nil {
		t.Fatalf("new_entity tool: %v", err)
	}
	res, err := tools.ModuleOp(context.Background(), ModuleOpArgs{
		MatchID: id, Module: "ccg", Op: "size", Args: map[string]any{"zone": "deck"},
	})
	if err != nil {
		t.Fatalf("size tool: %v", err)
	}
	if res.Result.(int64) != 1 {
		t.Fatalf("got deck size %v via tool, want 1", res.Result)
	}
}

// A spec whose setup leaves modules empty so the tool calls drive all state.
const miniCCGSpecForTools = `
META = {"name": "mini-ccg", "min_players": 1, "max_players": 1}
MODULES = ["ccg"]
def setup(ctx): return {}
def end_if(state, ctx): return None
def legal_moves(state, ctx): return []
MOVES = {"noop": {"apply": lambda state, ctx: state}}
`
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd mcp && go test . -run TestModuleTool_ParityWithStarlark -v`
Expected: FAIL — `Tools.ModuleOp` / `ModuleOpArgs` undefined.

- [ ] **Step 3: Add the dispatcher to tools.go**

```go
// mcp/tools.go
import "github.com/tjcran/boardgame-go/mcp/modulebridge"

// ModuleOpArgs invokes one engine-module op against a draft match's live
// module state. Design-time only: it lets Claude prototype mechanics on a
// draft game interactively. Match-scoped + ownership-checked like every
// other match tool.
type ModuleOpArgs struct {
	MatchID string         `json:"matchId"`
	Module  string         `json:"module"`
	Op      string         `json:"op"`
	Args    map[string]any `json:"args,omitempty"`
}

type ModuleOpResult struct {
	Result any `json:"result,omitempty"`
}

func (t *Tools) ModuleOp(ctx context.Context, args ModuleOpArgs) (ModuleOpResult, error) {
	if err := t.requireOwnership(ctx, args.MatchID); err != nil {
		return ModuleOpResult{}, err
	}
	reg := modulebridge.RegistryFor(args.Module)
	if reg == nil {
		return ModuleOpResult{}, fmt.Errorf("unknown module %q", args.Module)
	}
	var chosen *modulebridge.Op
	for _, op := range reg.Ops(args.Module) {
		if op.Name == args.Op {
			op := op
			chosen = &op
			break
		}
	}
	if chosen == nil {
		return ModuleOpResult{}, fmt.Errorf("unknown op %q for module %q", args.Op, args.Module)
	}

	m, err := t.Manager.State(args.MatchID)
	if err != nil {
		return ModuleOpResult{}, err
	}
	sg, ok := m.State.G.(*starlarkgame.StarlarkG)
	if !ok {
		return ModuleOpResult{}, fmt.Errorf("match %s is not a designed game", args.MatchID)
	}
	st, ok := sg.Modules[args.Module]
	if !ok {
		return ModuleOpResult{}, fmt.Errorf("match %s did not declare module %q", args.MatchID, args.Module)
	}
	res, err := chosen.Call(st, args.Args)
	if err != nil {
		return ModuleOpResult{}, err
	}
	return ModuleOpResult{Result: res}, nil
}
```

> Note: `requireOwnership` already exists in `mcp/` (see `ownership.go` / `tools_useraware_test.go`); in single-tenant/stdio mode with `Ownership == nil` it is a no-op. Confirm its exact name with `grep -n "requireOwnership" mcp/*.go` and match it.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd mcp && go test . -run TestModuleTool_ParityWithStarlark -v`
Expected: PASS.

- [ ] **Step 5: Register the tool in the MCP SDK glue (design-time gating)**

In `server.go` where tools are registered, register `ModuleOp` only when the server is in design/draft mode. Confirm the existing registration pattern with `grep -n "AddTool\|RegisterTool\|register" mcp/server.go`, then add the `module_op` tool alongside `playtest_draft` (the other design-time tool). Gate it behind the same condition `playtest_draft` uses so it is not exposed to plain players.

- [ ] **Step 6: Run the full mcp suite**

Run: `cd mcp && go test ./... -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add mcp/tools.go mcp/tools_modules_test.go mcp/server.go
git commit -m "mcp: design-time module_op tool sharing the modulebridge registry"
```

---

## Task 8: Documentation + validate-time consistency check

**Files:**
- Modify: `mcp/starlarkgame/validate.go` (reject `ctx.modules.<name>` use when `<name>` not in `MODULES` — best-effort static scan)
- Modify: `mcp/docs/designed-games.md` (document `MODULES` + `ctx.modules.ccg.*`)
- Test: `mcp/starlarkgame/validate_test.go`

- [ ] **Step 1: Write the failing test**

```go
// append to mcp/starlarkgame/validate_test.go
func TestValidate_RejectsUndeclaredModuleUse(t *testing.T) {
	src := `
META = {"name": "m", "min_players": 1, "max_players": 1}
def setup(ctx):
    ctx.modules.ccg.new_zone(name="hand", ordered=False)
    return {}
def end_if(state, ctx): return None
def legal_moves(state, ctx): return []
MOVES = {"noop": {"apply": lambda state, ctx: state}}
`
	spec, err := LoadSpec(src)
	if err != nil {
		t.Fatalf("LoadSpec: %v", err)
	}
	if err := Validate(context.Background(), spec); err == nil {
		t.Fatal("expected Validate to reject ctx.modules.ccg use without MODULES=[\"ccg\"]")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd mcp && go test ./starlarkgame/ -run TestValidate_RejectsUndeclaredModuleUse -v`
Expected: FAIL — Validate currently returns nil (the call only fails at runtime when `ctx.modules` is absent).

- [ ] **Step 3: Add the static scan**

Add to `Validate` (after the existing smoke checks) a source scan over `spec.source` for `ctx.modules.<name>` references and require each `<name>` be in `spec.Modules`:

```go
// validate.go
import "regexp"

var moduleRefRe = regexp.MustCompile(`ctx\.modules\.([a-z_]+)`)

func checkModuleRefs(s *Spec) error {
	declared := map[string]bool{}
	for _, m := range s.Modules {
		declared[m] = true
	}
	for _, match := range moduleRefRe.FindAllStringSubmatch(s.Source(), -1) {
		if !declared[match[1]] {
			return fmt.Errorf("spec uses ctx.modules.%s but %q is not in MODULES", match[1], match[1])
		}
	}
	return nil
}
```

Call `checkModuleRefs(spec)` inside `Validate` and return its error. If `Spec.source` is unexported with no accessor, add a small `func (s *Spec) Source() string { return s.source }` to `spec.go`.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd mcp && go test ./starlarkgame/ -run TestValidate_RejectsUndeclaredModuleUse -v`
Expected: PASS.

- [ ] **Step 5: Document the feature**

In `mcp/docs/designed-games.md`, under "Engine limits to know about", add a subsection:

```markdown
## Using engine modules (MODULES)

Declare engine modules your game uses with a top-level `MODULES` list and call
them through `ctx.modules.<name>.*` inside setup/apply:

    MODULES = ["ccg"]

    def setup(ctx):
        ctx.modules.ccg.new_zone(name="deck", ordered=True)
        ctx.modules.ccg.new_zone(name="hand", ordered=False)
        for i in range(40):
            c = ctx.modules.ccg.new_entity(type="card", owner="0")
            ctx.modules.ccg.move_to(entity=c, zone="deck")
        return {}

    def draw(state, ctx):
        top = ctx.modules.ccg.draw(zone="deck", n=1)
        ctx.modules.ccg.move_to(entity=top[0], zone="hand")
        return state

Entities are referenced by opaque tokens (e.g. `"ent:7"`); pass them back to
module ops, don't parse them. Phase 1 ships the `ccg` module (zones, entities,
move/draw/size/members). tabletop, economy/shop, target selection, and event
hooks land in later phases.
```

- [ ] **Step 6: Run the full suite + commit**

Run: `cd mcp && go test ./... && cd .. && go build ./...`
Expected: PASS / clean build.

```bash
git add mcp/starlarkgame/validate.go mcp/starlarkgame/spec.go mcp/starlarkgame/validate_test.go mcp/docs/designed-games.md
git commit -m "starlarkgame: validate MODULES references + document ctx.modules"
```

---

## Self-Review

**Spec coverage:**
- `StarlarkG` / module-state-in-G → Task 1. ✓
- `MODULES` declaration → Task 2. ✓
- `modulebridge` registry + handle table → Task 3. ✓
- ccg binding (the phase-1 module) → Tasks 4-5. ✓
- `ctx.modules.<name>.<op>()` runtime surface → Task 5. ✓
- Determinism guard + replay → Task 6 (replay test); the "no nondeterministic ops" rule is enforced by only registering pure ops in Task 4. ✓
- Dual MCP surface + parity test → Task 7. ✓
- Validate-time consistency check → Task 8. ✓
- Out-of-scope (tabletop, economy/shop, TargetRequest, HOOKS, semantic args) → explicitly deferred to later phases. ✓

**Placeholder scan:** No TBD/TODO. Two steps (Task 6 Step 2, Task 7 Step 5) instruct the engineer to confirm an existing API signature with a specific `grep` before finalizing — these are verification steps with the exact command given, not placeholders.

**Type consistency:** `StarlarkG{Data, Modules}`, `Op{Module,Name,MCPTool,Call}`, `Registry.Ops(module)`, `modulebridge.NewState`/`RegistryFor`/`RegistryFor`, `EntityToken`/`ParseEntityToken`, `BridgeCtx.Modules`, `Tools.ModuleOp`/`ModuleOpArgs`/`ModuleOpResult` are used consistently across tasks.

**Risk note for the executor:** The single load-bearing assumption is that the in-memory store shares the `State.G` pointer across moves (`storage/memory.go:cloneMatch`). Task 6 verifies this empirically. If a serializing store is used, module ID counters reset on reload — a pre-existing `ccg` limitation documented in the spec, out of scope here.
