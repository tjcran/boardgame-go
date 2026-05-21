# Engine Modules in Starlark — Phase 6 (HOOKS event-bus handlers) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let designed Starlark games react to ccg domain events — `HOOKS = {"creature_died": fn}` registers a handler that fires when a move publishes that event via `ctx.modules.ccg.publish(...)`, enabling "when X happens, do Y" triggered abilities.

**Architecture:** A top-level `HOOKS` dict maps a ccg event type to a Starlark handler `fn(event, ctx)`. Specs fire events with a new `ctx.modules.ccg.publish(type=, source=, target=, data=)` op (calls `ccg.State.Publish`, which routes synchronously to subscribers). Hooks are registered in `g.Setup` (after `CallSetup`) by `Subscribe(MatchType(type), goHandler)` on the live ccg state, in deterministic `HOOKS` order. The hard part — a ccg subscriber `func(*State, Event)` fires inside `Publish` with no Starlark thread and inside the move's own Starlark execution — is solved by having each handler invocation **spawn a fresh sandboxed Starlark thread** (Starlark functions close over module globals, not a thread) and mutate the **shared live module state**. A per-match depth counter bounds re-entrant cascades. Handler errors cross the `Publish` callback boundary via a `modulebridge.HookError` panic that the `publish` op recovers into a normal error return, so a failing hook rejects the move instead of vanishing.

**Tech Stack:** Go, `go.starlark.net`, `modules/ccg` (event bus), `mcp/modulebridge`, `mcp/starlarkgame`.

**Reference spec:** `docs/superpowers/specs/2026-05-21-engine-modules-in-starlark-and-mcp.md` — see "Phase 6 — HOOKS (event-bus reactive handlers) design".

**Key engine facts (verified):**
- `ccg.Event{Type string; Source, Target EntityID; Data map[string]any}`. `(*ccg.State).Publish(Event)` routes synchronously, depth-first, to subscribers. `(*ccg.State).Subscribe(pred EventPredicate, h EventHandler) SubscriptionID`; `EventHandler = func(*State, Event)`; `ccg.MatchType(t string) EventPredicate`.
- ccg subscribers are NOT serialized (in-memory only); re-registered every process start / Setup. The in-memory store keeps the live G across moves so subs persist; replay re-runs Setup so it re-registers — deterministic.
- `(*Spec).newThread(ctx context.Context) *starlark.Thread` (in `call.go`) builds a sandboxed thread (load disabled, step cap). Call a callable with `starlark.Call(thread, fn, starlark.Tuple{...}, nil)`.
- `BridgeCtx.asStarlark()` exposes `ctx.modules.<name>.<op>()` when `BridgeCtx.Modules` is non-empty (phase 1/3). So a handler ctx built as `&BridgeCtx{Modules: mods}` gives the handler `ctx.modules.*`.
- `modulebridge.Op.Call(modules map[string]any, args map[string]any) (any, error)` (phase 3). ccg ops pull state via `ccgFrom(modules)`. `modulebridge.EntityToken(ccg.EntityID) string` / `ParseEntityToken`.
- `g.Setup` (game.go) instantiates `mods` from `s.Modules` via `modulebridge.NewState`, runs `s.CallSetup`, returns `&StarlarkG{Data, Modules: mods}`.

**Scope note:** Only the ccg event bus is bridged (not the Effect envelope). Handlers mutate **module state only** — they do not receive/mutate the user `Data` dict (frozen during a move). HOOKS requires `ccg` in `MODULES`.

---

## File Structure

- `mcp/modulebridge/ccg.go` (modify) — add the `publish` op (with `HookError` recover) ; `mcp/modulebridge/hookerror.go` (new) — the `HookError` type.
- `mcp/modulebridge/ccg_test.go` (modify) — unit test for `publish` firing a Go subscriber.
- `mcp/starlarkgame/spec.go` (modify) — parse `HOOKS` into `Spec.Hooks []Hook` (ordered); require `ccg`.
- `mcp/starlarkgame/hooks.go` (new) — `registerHooks(s, mods)` + `goHandler` (fresh thread, event dict, depth guard, panic-on-error).
- `mcp/starlarkgame/game.go` (modify) — call `registerHooks` in `g.Setup` after `CallSetup`.
- `mcp/starlarkgame/hooks_test.go` (new) — bridge-level: publishing fires a Starlark hook that mutates ccg state; a raising hook surfaces an error.
- `mcp/starlarkgame/modules_hooks_test.go` (new) — integration: publish "died" → hook draws a card; through Manager + replay.
- `mcp/docs/designed-games.md` (modify) — document HOOKS.

---

## Task 1: `ctx.modules.ccg.publish` op + `HookError`

**Files:** create `mcp/modulebridge/hookerror.go`; modify `mcp/modulebridge/ccg.go`, `mcp/modulebridge/ccg_test.go`.

- [ ] **Step 1: Write the failing test (append to `mcp/modulebridge/ccg_test.go`)**

```go
func TestCCG_Publish_FiresSubscriber(t *testing.T) {
	st := ccg.NewState()
	fired := 0
	st.Subscribe(ccg.MatchType("died"), func(_ *ccg.State, e ccg.Event) {
		fired++
	})
	mods := map[string]any{"ccg": st}
	if _, err := ccgOp(t, "publish").Call(mods, map[string]any{"type": "died"}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if fired != 1 {
		t.Fatalf("subscriber fired %d times, want 1", fired)
	}
}

func TestCCG_Publish_RecoversHookError(t *testing.T) {
	st := ccg.NewState()
	st.Subscribe(ccg.MatchType("boom"), func(_ *ccg.State, e ccg.Event) {
		panic(HookError{Err: errTestHook})
	})
	mods := map[string]any{"ccg": st}
	_, err := ccgOp(t, "publish").Call(mods, map[string]any{"type": "boom"})
	if err == nil {
		t.Fatal("expected publish to surface the HookError panic as an error")
	}
}

var errTestHook = fmt.Errorf("boom hook failed")
```
(Add `"fmt"` to the test file's imports if not already present.)

- [ ] **Step 2: Confirm failure** — `export PATH=$HOME/.local/go/bin:$PATH && cd mcp && go test ./modulebridge/ -run TestCCG_Publish -v` → FAIL (`publish` op + `HookError` undefined).

- [ ] **Step 3: Create `mcp/modulebridge/hookerror.go`**

```go
package modulebridge

// HookError wraps a hook-handler failure so it can cross the
// ccg.Publish callback boundary (subscribers are func(*State, Event)
// with no error return). The starlarkgame hook handler panics with a
// HookError on a Starlark error; the publish op recovers it into a
// normal error return, so a failing hook rejects the move.
type HookError struct{ Err error }

func (h HookError) Error() string {
	if h.Err == nil {
		return "modulebridge: hook handler failed"
	}
	return "hook handler: " + h.Err.Error()
}
```

- [ ] **Step 4: Add the `publish` op to `buildCCGRegistry` in `ccg.go`** (before `return r`)

```go
	r.Add(Op{Module: "ccg", Name: "publish", MCPTool: "ccg_publish",
		Call: func(modules map[string]any, args map[string]any) (res any, err error) {
			s, e := ccgFrom(modules)
			if e != nil {
				return nil, e
			}
			typ, e := argStr(args, "type")
			if e != nil {
				return nil, e
			}
			ev := ccg.Event{Type: typ}
			if tok, ok := args["source"].(string); ok && tok != "" {
				id, e := ParseEntityToken(tok)
				if e != nil {
					return nil, e
				}
				ev.Source = id
			}
			if tok, ok := args["target"].(string); ok && tok != "" {
				id, e := ParseEntityToken(tok)
				if e != nil {
					return nil, e
				}
				ev.Target = id
			}
			if d, ok := args["data"].(map[string]any); ok {
				ev.Data = d
			}
			// Recover a hook handler's HookError panic (handlers can't
			// return errors across ccg.Publish) into a normal error.
			defer func() {
				if r := recover(); r != nil {
					if he, ok := r.(HookError); ok {
						err = he
						return
					}
					panic(r)
				}
			}()
			s.Publish(ev)
			return nil, nil
		}})
```

- [ ] **Step 5: Confirm pass** — `... go test ./modulebridge/ -run TestCCG_Publish -v` → PASS. Then `... go test ./modulebridge/` → ok.

- [ ] **Step 6: Commit**

```bash
cd mcp && git add modulebridge/hookerror.go modulebridge/ccg.go modulebridge/ccg_test.go
git commit -m "modulebridge: ccg.publish op + HookError for hook-failure propagation"
```

---

## Task 2: Parse `HOOKS` (ordered) + require ccg

**Files:** modify `mcp/starlarkgame/spec.go`, `mcp/starlarkgame/spec_test.go`.

- [ ] **Step 1: Append failing tests to `mcp/starlarkgame/spec_test.go`**

```go
func TestLoadSpec_ParsesHooks(t *testing.T) {
	s, err := LoadSpec(`
META = {"name":"h","min_players":1,"max_players":1}
MODULES = ["ccg"]
def setup(ctx): return {}
def end_if(state, ctx): return None
def legal_moves(state, ctx): return []
def on_died(event, ctx): pass
def on_drawn(event, ctx): pass
HOOKS = {"died": on_died, "drawn": on_drawn}
MOVES = {"noop": {"apply": lambda s, c: s}}
`)
	if err != nil {
		t.Fatalf("LoadSpec: %v", err)
	}
	if len(s.Hooks) != 2 || s.Hooks[0].Type != "died" || s.Hooks[1].Type != "drawn" {
		t.Fatalf("hooks wrong/!ordered: %#v", s.Hooks)
	}
}

func TestLoadSpec_HooksRequireCCG(t *testing.T) {
	_, err := LoadSpec(`
META = {"name":"h","min_players":1,"max_players":1}
def setup(ctx): return {}
def end_if(state, ctx): return None
def legal_moves(state, ctx): return []
def on_died(event, ctx): pass
HOOKS = {"died": on_died}
MOVES = {"noop": {"apply": lambda s, c: s}}
`)
	if err == nil {
		t.Fatal("expected HOOKS without ccg in MODULES to be rejected")
	}
}
```

- [ ] **Step 2: Confirm failure** — `export PATH=$HOME/.local/go/bin:$PATH && cd mcp && go test ./starlarkgame/ -run TestLoadSpec_ParsesHooks -v` and `-run TestLoadSpec_HooksRequireCCG` → FAIL (`s.Hooks` undefined).

- [ ] **Step 3: Add `Hook`/`Spec.Hooks` and parse (spec.go)**

Add the type and field:
```go
// Hook binds a ccg event type to a Starlark handler fn(event, ctx).
type Hook struct {
	Type string
	Fn   starlark.Callable
}
```
Add to the `Spec` struct (near `Modules`):
```go
	// Hooks is the optional ordered HOOKS table: ccg event type -> handler.
	// Requires "ccg" in Modules. Registered on the live ccg state in Setup.
	Hooks []Hook
```
In `LoadSpec`, before `return s, nil` and AFTER `readModules`, add:
```go
	if err := readHooks(globals, s); err != nil {
		return nil, err
	}
```
Add the parser (preserve insertion order via `Dict.Keys()`):
```go
func readHooks(globals starlark.StringDict, s *Spec) error {
	raw, ok := globals["HOOKS"]
	if !ok {
		return nil
	}
	d, ok := raw.(*starlark.Dict)
	if !ok {
		return fmt.Errorf("HOOKS must be a dict, got %s", raw.Type())
	}
	if d.Len() == 0 {
		return nil
	}
	hasCCG := false
	for _, m := range s.Modules {
		if m == "ccg" {
			hasCCG = true
		}
	}
	if !hasCCG {
		return fmt.Errorf("HOOKS requires \"ccg\" in MODULES")
	}
	for _, k := range d.Keys() {
		ks, ok := k.(starlark.String)
		if !ok {
			return fmt.Errorf("HOOKS key %v must be a string", k)
		}
		v, _, _ := d.Get(k)
		fn, ok := v.(starlark.Callable)
		if !ok {
			return fmt.Errorf("HOOKS[%q] must be a function, got %s", string(ks), v.Type())
		}
		s.Hooks = append(s.Hooks, Hook{Type: string(ks), Fn: fn})
	}
	return nil
}
```
(`starlark` and `fmt` are already imported in spec.go.)

- [ ] **Step 4: Confirm pass** — both `-run TestLoadSpec_ParsesHooks` and `-run TestLoadSpec_HooksRequireCCG` → PASS. Full: `... go test ./starlarkgame/` → ok.

- [ ] **Step 5: Commit**

```bash
cd mcp && git add starlarkgame/spec.go starlarkgame/spec_test.go
git commit -m "starlarkgame: parse ordered HOOKS table; require ccg module"
```

---

## Task 3: Register hooks in Setup + fresh-thread handler

**Files:** create `mcp/starlarkgame/hooks.go`, `mcp/starlarkgame/hooks_test.go`; modify `mcp/starlarkgame/game.go`.

- [ ] **Step 1: Write the failing test `mcp/starlarkgame/hooks_test.go`**

```go
package starlarkgame

import (
	"context"
	"testing"

	"github.com/tjcran/boardgame-go/modules/ccg"
)

// Registering a HOOKS handler then publishing the event should run the
// handler, which mutates live ccg state (here: moves a card deck->hand).
func TestHooks_FireOnPublishMutateState(t *testing.T) {
	spec, err := LoadSpec(`
META = {"name":"h","min_players":1,"max_players":1}
MODULES = ["ccg"]
def setup(ctx):
    ctx.modules.ccg.new_zone(name="deck", ordered=True)
    ctx.modules.ccg.new_zone(name="hand", ordered=False)
    c = ctx.modules.ccg.new_entity(type="card", owner="0")
    ctx.modules.ccg.move_to(entity=c, zone="deck")
    return {}
def end_if(state, ctx): return None
def legal_moves(state, ctx): return []
def on_bell(event, ctx):
    top = ctx.modules.ccg.draw(zone="deck", n=1)
    ctx.modules.ccg.move_to(entity=top[0], zone="hand")
HOOKS = {"bell": on_bell}
def ring(state, ctx):
    ctx.modules.ccg.publish(type="bell")
    return state
MOVES = {"ring": {"apply": ring}}
`)
	if err != nil {
		t.Fatalf("LoadSpec: %v", err)
	}
	g := BuildCoreGame(spec)
	sg := g.Setup(coreCtx(1), nil).(*StarlarkG) // coreCtx helper from modules_ccg_test.go
	st := sg.Modules["ccg"].(*ccg.State)
	if st.Size("deck") != 1 || st.Size("hand") != 0 {
		t.Fatalf("setup: deck=%d hand=%d", st.Size("deck"), st.Size("hand"))
	}

	// Run the "ring" move, which publishes "bell"; the hook should move the
	// card deck->hand.
	bc := &BridgeCtx{NumPlayers: 1, Modules: sg.Modules}
	if _, err := spec.CallMove(context.Background(), bc, "ring", sg.Data, nil); err != nil {
		t.Fatalf("ring move: %v", err)
	}
	if st.Size("deck") != 0 || st.Size("hand") != 1 {
		t.Fatalf("after hook: deck=%d hand=%d, want 0/1", st.Size("deck"), st.Size("hand"))
	}
}

// A handler that raises surfaces as a move error (HookError propagation).
func TestHooks_HandlerErrorRejectsMove(t *testing.T) {
	spec, err := LoadSpec(`
META = {"name":"h","min_players":1,"max_players":1}
MODULES = ["ccg"]
def setup(ctx): return {}
def end_if(state, ctx): return None
def legal_moves(state, ctx): return []
def on_boom(event, ctx):
    fail("kaboom")
HOOKS = {"boom": on_boom}
def trigger(state, ctx):
    ctx.modules.ccg.publish(type="boom")
    return state
MOVES = {"trigger": {"apply": trigger}}
`)
	if err != nil {
		t.Fatalf("LoadSpec: %v", err)
	}
	g := BuildCoreGame(spec)
	sg := g.Setup(coreCtx(1), nil).(*StarlarkG)
	bc := &BridgeCtx{NumPlayers: 1, Modules: sg.Modules}
	if _, err := spec.CallMove(context.Background(), bc, "trigger", sg.Data, nil); err == nil {
		t.Fatal("expected the raising hook to reject the move")
	}
}
```
Note: `coreCtx` already exists in `modules_ccg_test.go` (same package). The publishing path in these tests goes through `CallMove` with a `BridgeCtx` whose `Modules` is the same map registered in `g.Setup`, so the subscribers (registered on `sg.Modules["ccg"]`) fire.

IMPORTANT subtlety: `g.Setup` must register the hooks on the SAME `*ccg.State` that the later `CallMove` publishes against. Since both use `sg.Modules` (the map returned by Setup), and `CallMove`'s `ctx.modules.ccg.publish` resolves `modules["ccg"]` to that same live state, the subscriber fires. Confirm the test passes for this reason; if it doesn't fire, the registration isn't on the returned map's state.

- [ ] **Step 2: Confirm failure** — `export PATH=$HOME/.local/go/bin:$PATH && cd mcp && go test ./starlarkgame/ -run TestHooks -v` → FAIL (hooks never registered; `on_bell` doesn't run).

- [ ] **Step 3: Create `mcp/starlarkgame/hooks.go`**

```go
package starlarkgame

import (
	"context"

	"github.com/tjcran/boardgame-go/mcp/modulebridge"
	"github.com/tjcran/boardgame-go/modules/ccg"
	"go.starlark.net/starlark"
)

// maxHookDepth bounds re-entrant hook cascades (a handler publishing an
// event that re-triggers handlers), mirroring core's chained-move cap.
const maxHookDepth = 32

// registerHooks subscribes each HOOKS handler on the live ccg state in
// mods. Handlers fire synchronously inside ccg.Publish; each invocation
// runs on a FRESH sandboxed Starlark thread (Starlark functions close
// over module globals, not a thread) and mutates the shared live module
// state via ctx.modules.*. A shared depth counter bounds recursion. A
// handler's Starlark error is raised as a modulebridge.HookError panic,
// which the ccg.publish op recovers into a move-rejecting error.
func registerHooks(s *Spec, mods map[string]any) {
	if len(s.Hooks) == 0 {
		return
	}
	cs, ok := mods["ccg"].(*ccg.State)
	if !ok {
		return
	}
	depth := 0
	for _, h := range s.Hooks {
		fn := h.Fn
		cs.Subscribe(ccg.MatchType(h.Type), func(_ *ccg.State, e ccg.Event) {
			if depth >= maxHookDepth {
				panic(modulebridge.HookError{Err: errHookDepth})
			}
			depth++
			defer func() { depth-- }()

			bc := &BridgeCtx{Modules: mods}
			ev := eventToDict(e)
			sv, err := ToStarlark(ev)
			if err != nil {
				panic(modulebridge.HookError{Err: err})
			}
			thread := s.newThread(context.Background())
			if _, err := starlark.Call(thread, fn, starlark.Tuple{sv, bc.asStarlark()}, nil); err != nil {
				panic(modulebridge.HookError{Err: err})
			}
		})
	}
}

var errHookDepth = errHookDepthError{}

type errHookDepthError struct{}

func (errHookDepthError) Error() string { return "hook recursion exceeded maxHookDepth" }

func eventToDict(e ccg.Event) map[string]any {
	out := map[string]any{"type": e.Type}
	if e.Source != 0 {
		out["source"] = modulebridge.EntityToken(e.Source)
	}
	if e.Target != 0 {
		out["target"] = modulebridge.EntityToken(e.Target)
	}
	if e.Data != nil {
		out["data"] = map[string]any(e.Data)
	}
	return out
}
```

- [ ] **Step 4: Call `registerHooks` in `g.Setup` (game.go)**

In the `g.Setup` closure, after `data, err := s.CallSetup(...)` and the error branch, register hooks before returning the success `StarlarkG`. Both return paths build the `StarlarkG` with `mods`; register on the success path (and it's fine to register on the error path too, but the error state immediately fails end_if so skip it). Concretely, change the tail:
```go
		sg := &StarlarkG{Data: data, Modules: mods}
		registerHooks(s, mods)
		return sg
```
(Register AFTER CallSetup so initial setup doesn't trigger hooks; the live `mods["ccg"]` is the same state the match keeps across moves.)

- [ ] **Step 5: Confirm pass** — `... go test ./starlarkgame/ -run TestHooks -v` → PASS (both). Full: `... go test ./starlarkgame/ ./modulebridge/` → ok / ok.

- [ ] **Step 6: Commit**

```bash
cd mcp && git add starlarkgame/hooks.go starlarkgame/game.go starlarkgame/hooks_test.go
git commit -m "starlarkgame: register HOOKS on ccg event bus; fresh-thread handlers with depth guard"
```

---

## Task 4: End-to-end through Manager + replay determinism

**Files:** create `mcp/starlarkgame/modules_hooks_test.go`.

- [ ] **Step 1: Write the test**

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

// A 1-player spec: a 2-card deck + empty hand + a graveyard. The "slay"
// move destroys-by-moving a board creature to graveyard and publishes
// "died"; a HOOKS handler on "died" draws a card to hand. After one slay,
// hand has 1 card. Proves hooks fire through the real Manager + replay.
const hooksGameSpec = `
META = {"name": "hooksgame", "min_players": 1, "max_players": 1}
MODULES = ["ccg"]

def setup(ctx):
    ctx.modules.ccg.new_zone(name="deck", ordered=True)
    ctx.modules.ccg.new_zone(name="hand", ordered=False)
    ctx.modules.ccg.new_zone(name="board", ordered=False)
    ctx.modules.ccg.new_zone(name="grave", ordered=False)
    for i in range(2):
        c = ctx.modules.ccg.new_entity(type="card", owner="0")
        ctx.modules.ccg.move_to(entity=c, zone="deck")
    cr = ctx.modules.ccg.new_entity(type="creature", owner="0")
    ctx.modules.ccg.move_to(entity=cr, zone="board")
    return {"creature": cr}

def end_if(state, ctx): return None
def legal_moves(state, ctx): return [{"name": "slay", "args": []}]

def on_died(event, ctx):
    top = ctx.modules.ccg.draw(zone="deck", n=1)
    ctx.modules.ccg.move_to(entity=top[0], zone="hand")

HOOKS = {"died": on_died}

def slay(state, ctx):
    cr = state["creature"]
    ctx.modules.ccg.move_to(entity=cr, zone="grave")
    ctx.modules.ccg.publish(type="died", source=cr)
    return state

MOVES = {"slay": {"apply": slay}}
`

func TestHooksGame_SlayDrawsAndReplays(t *testing.T) {
	spec, err := LoadSpec(hooksGameSpec)
	if err != nil {
		t.Fatalf("LoadSpec: %v", err)
	}
	if err := Validate(context.Background(), spec); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	g := BuildCoreGame(spec)
	mgr := match.NewManager(storage.NewMemory())
	mgr.MustRegister(g)
	id, _ := mgr.Create("hooksgame", match.CreateOptions{NumPlayers: 1})
	jr, _ := mgr.Join(id, "p", match.JoinOptions{})

	if _, err := mgr.MoveReq(id, jr.PlayerID, jr.PlayerCredentials, core.MoveRequest{Move: "slay"}); err != nil {
		t.Fatalf("slay: %v", err)
	}
	m, _ := mgr.State(id)
	sg := m.State.G.(*StarlarkG)
	ccgState := ccgStateOf(t, sg) // helper from modules_ccg_test.go
	if ccgState.Size("hand") != 1 || ccgState.Size("deck") != 1 || ccgState.Size("grave") != 1 {
		t.Fatalf("after slay: hand=%d deck=%d grave=%d, want 1/1/1",
			ccgState.Size("hand"), ccgState.Size("deck"), ccgState.Size("grave"))
	}

	// Replay re-runs Setup (re-registers the hook) and the slay move; the
	// hook fires again deterministically -> byte-identical final G.
	replayed, err := core.Replay(g, m.State.Log, 1, nil)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	orig, _ := json.Marshal(m.State.G)
	repl, _ := json.Marshal(replayed.G)
	if string(orig) != string(repl) {
		t.Fatalf("replay mismatch:\n orig=%s\n repl=%s", orig, repl)
	}
}
```

- [ ] **Step 2: Run it** — `export PATH=$HOME/.local/go/bin:$PATH && cd mcp && go test ./starlarkgame/ -run TestHooksGame -v`.
  - **Compile errors**: cross-check the Manager/replay API against `modules_replay_test.go`; adjust. Re-run.
  - **PASS**: commit.
  - **Assertion failure**: do NOT weaken. Diagnose:
    - hand stayed 0 → the hook didn't fire: confirm `registerHooks` runs in `g.Setup` on `mods["ccg"]` and that the `publish` op resolves the same live state (it does via `modules["ccg"]`).
    - replay mismatch → hooks must re-register on replay's Setup; since `core.Replay` calls the game's `Setup`, `registerHooks` runs again. If subscribers somehow doubled (registered twice on the same state), the hook would fire twice — but each match/replay uses a FRESH ccg state from `NewState`, so no doubling. Report the orig/repl strings if it mismatches.

- [ ] **Step 3: Full suites** — `... go test ./starlarkgame/ ./modulebridge/` → ok / ok.

- [ ] **Step 4: Commit**

```bash
cd mcp && git add starlarkgame/modules_hooks_test.go
git commit -m "starlarkgame: e2e HOOKS (on-died draws) + replay determinism test"
```

---

## Task 5: Documentation

**Files:** modify `mcp/docs/designed-games.md`.

- [ ] **Step 1: Add a HOOKS subsection** after the "Semantic argument types" section (and update the trailing "Event hooks (HOOKS) land in a later phase." line, which should be removed/replaced since hooks now exist). Content:

```markdown
## Reacting to events (HOOKS)

Declare `HOOKS` to react to ccg domain events — triggered abilities like "when a
creature dies, draw a card." A `HOOKS` entry maps an event type to a handler
`fn(event, ctx)`; the handler reacts via `ctx.modules.*`. Requires `ccg` in
MODULES. Fire events from a move with `ctx.modules.ccg.publish(type=...,
source=..., target=..., data=...)`.

    MODULES = ["ccg"]

    def on_died(event, ctx):
        # event = {"type": "died", "source": "ent:3", ...}
        top = ctx.modules.ccg.draw(zone="deck", n=1)
        ctx.modules.ccg.move_to(entity=top[0], zone="hand")

    HOOKS = {"died": on_died}

    def slay(state, ctx):
        ctx.modules.ccg.move_to(entity=state["creature"], zone="grave")
        ctx.modules.ccg.publish(type="died", source=state["creature"])
        return state

`event` is a dict `{type, source?, target?, data?}` (source/target are entity
tokens when present). Handlers may only mutate **module state** (zones,
counters) via `ctx.modules` — they do not receive the game's user `data` dict
(it is frozen during a move). Handlers fire synchronously and may publish
further events (bounded against runaway recursion). If a handler raises
(`fail(...)`), the triggering move is rejected.
```

- [ ] **Step 2: Build + full test + commit**

```bash
export PATH=$HOME/.local/go/bin:$PATH && cd mcp && go build ./... && go test ./starlarkgame/ ./modulebridge/
```
Expected: clean + green.
```bash
cd mcp && git add docs/designed-games.md && git commit -m "docs: document HOOKS event handlers for designed games"
```

---

## Self-Review

**Spec coverage:** HOOKS delivered per the phase-6 design section — ordered registration, `ctx.modules.ccg.publish`, fresh-thread handlers mutating module state, depth guard, error propagation, ccg dependency. ✓

**Placeholder scan:** No TBD/TODO; Task 4 debug guidance is conditional with concrete checks. ✓

**Type consistency:** `Spec.Hooks []Hook` (`Hook{Type string; Fn starlark.Callable}`); `registerHooks(s *Spec, mods map[string]any)`; `eventToDict(ccg.Event) map[string]any`; `modulebridge.HookError{Err error}`; the `publish` op recover matches the `HookError` panic raised by `registerHooks`. ✓

**Determinism:** Hooks register in deterministic order on a fresh per-match ccg state; handlers run on fresh threads but mutate shared state deterministically; replay re-runs Setup → re-registers → identical. Depth guard is deterministic. No RNG introduced. Replay proven in Task 4. ✓

**Re-entrancy safety:** Handlers never re-enter the move's live Starlark thread — each runs on its own thread. The only shared mutable is the Go module state (serialized per-match by the Manager lock). ✓

**Known caveats (carry forward):** (1) Handlers can't mutate user `Data` (frozen) — module-state reactions only. (2) Effect-envelope events (PushEffect/ResolveNext) are not bridged. (3) The depth guard, on overflow, raises a HookError that rejects the move (no silent truncation). The remaining initiative item after phase 6 is **seeded-RNG-to-ops threading** (shop shuffle, tabletop dice/combat) — the next requested work.
```
