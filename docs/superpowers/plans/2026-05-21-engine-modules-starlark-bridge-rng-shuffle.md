# Engine Modules in Starlark — Seeded RNG for ops (ccg.shuffle) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give `modulebridge` ops access to the move's seeded PRNG so RNG-consuming operations are replay-deterministic, and expose the first one — `ctx.modules.ccg.shuffle(zone=...)` — so designed games can shuffle a deck/zone (shuffle-on-setup, shuffle discard back into deck) deterministically.

**Architecture / context:** This is the one cross-cutting item left after the 6-phase bridge. Findings that shape it:
- The Starlark adapter does NOT use `core.MoveContext.Random` — it is **nil for designed games** (the random plugin isn't registered on `BuildCoreGame` games; `core/reducer.go:144` only populates `mc.Random` when that plugin is present). Registering the plugin would introduce a separate seed-persistence/replay problem, so we do NOT go there.
- The adapter already owns a replay-deterministic PRNG: `BridgeCtx.rng` (`*bridgeRandom`, wrapping a `*core.Random`), installed each call via `AttachSeededRandom(ctxSeed(ctx))` and exposed to Starlark as `ctx.random` (range/shuffle/choice). `ctxSeed` derives a stable seed from `(turn, numPlayers)`, so replay (which reconstructs the same turn/numPlayers) reproduces the same draws.
- ccg already has `(*ccg.State).Shuffle(zone ccg.ZoneName, r *core.Random) error` (uses `core.Shuffle`). It is simply not bridged yet (deliberately omitted in phase 1 as the lone RNG consumer).

So: thread the move's `BridgeCtx.rng`'s underlying `*core.Random` into `modulebridge.Op.Call` (add an `rng *core.Random` parameter — op bodies that don't need it keep using `modules` and ignore `rng`, which compiles since unused *parameters* are allowed), then add the `ccg.shuffle` op using it.

**Tech Stack:** Go, `go.starlark.net`, `modules/ccg`, `mcp/modulebridge`, `mcp/starlarkgame`, `core` (`Random`).

**Reference spec:** `docs/superpowers/specs/2026-05-21-engine-modules-in-starlark-and-mcp.md` — "seeded-RNG-to-ops threading" is the remaining cross-cutting item noted across phases 2/3/6.

**Determinism note (document, don't fix here):** `ctxSeed` is `f(turn, numPlayers)`, so RNG re-seeds identically at the start of every move within the same turn (a pre-existing property of `ctx.random`). Shuffle is therefore replay-deterministic but, within one turn, repeated shuffles of equal-size inputs draw the same sequence. Acceptable for v1; a per-move nonce in the seed is a possible future refinement. Out of scope here.

**Why not bundle into an OpCtx struct:** an `rng *core.Random` third parameter keeps every existing op body byte-identical (they keep reading `modules`); only the closure signature line changes. That is strictly less churn than rewriting every op to `oc.Modules`. YAGNI on a context struct until a second cross-cutting need appears.

---

## File Structure

- `mcp/modulebridge/registry.go` (modify) — `Op.Call` gains `rng *core.Random`.
- `mcp/modulebridge/ccg.go`, `tabletop.go`, `economy.go`, `shop.go` (modify) — every op closure's signature gains `rng *core.Random` (bodies unchanged; shop already passes `nil` to `Fill`/`Roll` and keeps doing so).
- `mcp/modulebridge/*_test.go` (modify) — every `.Call(modules, args)` becomes `.Call(modules, args, nil)`.
- `mcp/starlarkgame/bridge.go` (modify) — `modulesAsStarlark` passes the move's `*core.Random` (`c.rng.rng`, nil-guarded) to `op.Call`.
- `mcp/tools.go` (modify) — `ModuleOp` passes a fresh seeded `*core.Random` (so design-time shuffle is usable + deterministic).
- `mcp/modulebridge/ccg.go` (modify) — add the `shuffle` op (uses `rng`).
- `mcp/modulebridge/ccg_test.go` (modify) — shuffle unit + determinism tests.
- `mcp/starlarkgame/modules_shuffle_test.go` (new) — integration: shuffle a deck in a move; replay byte-identical.
- `mcp/docs/designed-games.md` (modify) — document `ccg.shuffle`.

---

## Task 1: Thread `rng *core.Random` through `Op.Call` (no new behavior)

Mechanical, compiler-checked, gated by the existing test suite staying green.

**Files:** `mcp/modulebridge/registry.go`, `ccg.go`, `tabletop.go`, `economy.go`, `shop.go`, all four `*_test.go`; `mcp/starlarkgame/bridge.go`, `mcp/tools.go`.

- [ ] **Step 1: Change the `Op.Call` field type in `registry.go`**

```go
import "github.com/tjcran/boardgame-go/core"

type Op struct {
	Module  string
	Name    string
	MCPTool string
	// Call receives all live module states, the op args, and the move's
	// seeded PRNG (nil at design-time / when no RNG is in scope). Ops that
	// don't need randomness ignore rng.
	Call func(modules map[string]any, args map[string]any, rng *core.Random) (any, error)
}
```
(Add the `core` import to registry.go.)

- [ ] **Step 2: Update every op closure signature in `ccg.go`, `tabletop.go`, `economy.go`, `shop.go`**

For EACH `r.Add(Op{... Call: func(modules map[string]any, args map[string]any) (any, error) {` change the closure signature to:
```go
		Call: func(modules map[string]any, args map[string]any, rng *core.Random) (any, error) {
```
Leave every body unchanged (they keep using `modules`; `rng` is an unused parameter, which compiles). Add the `core` import to any of these files that doesn't already have it (shop.go imports core already; check the others — add `"github.com/tjcran/boardgame-go/core"` where the build complains).

- [ ] **Step 3: Update `bridge.go` `modulesAsStarlark`**

Where it calls `op.Call(c.Modules, m)`, pass the move's PRNG. Add a nil-guarded local at the top of `modulesAsStarlark`:
```go
	var rng *core.Random
	if c.rng != nil {
		rng = c.rng.rng
	}
```
and change the call to `op.Call(c.Modules, m, rng)`. (`c.rng` is the `*bridgeRandom` set by `AttachSeededRandom`; `c.rng.rng` is its `*core.Random`. Both fields are in-package. Add the `core` import to bridge.go if not present.)

- [ ] **Step 4: Update `tools.go` `ModuleOp`**

Where it calls `chosen.Call(sg.Modules, args.Args)`, pass a fresh deterministic PRNG so design-time RNG ops work:
```go
	res, err := chosen.Call(sg.Modules, args.Args, core.NewRandomFromState(new(uint64)))
```
(`core` is already imported in tools.go. `core.NewRandomFromState(new(uint64))` makes a zero-seeded PRNG — fine for design-time poking; design-time calls are not part of the replayed match log.)

- [ ] **Step 5: Update all test call sites**

In `mcp/modulebridge/ccg_test.go`, `tabletop_test.go`, `economy_test.go`, `shop_test.go`, every `.Call(<modules>, <args>)` becomes `.Call(<modules>, <args>, nil)`. Grep each file for `.Call(` and append `, nil` to each call. (Do not change the modules/args themselves.)

- [ ] **Step 6: Build + full suites — nothing regresses**

Run: `export PATH=$HOME/.local/go/bin:$PATH && cd mcp && go build ./... && go vet ./modulebridge/ ./starlarkgame/ && go test ./modulebridge/ ./starlarkgame/ -v 2>&1 | tail -40`
Expected: ALL PASS (every ccg/tabletop/economy/shop unit test + the starlarkgame integration tests that exercise `op.Call` via `ctx.modules`). If a starlarkgame integration test fails, the `bridge.go` change is wrong — fix it, don't touch the tests.

- [ ] **Step 7: Commit**

```bash
cd mcp && git add modulebridge/registry.go modulebridge/ccg.go modulebridge/tabletop.go modulebridge/economy.go modulebridge/shop.go modulebridge/ccg_test.go modulebridge/tabletop_test.go modulebridge/economy_test.go modulebridge/shop_test.go starlarkgame/bridge.go tools.go
git commit -m "modulebridge: thread move's seeded *core.Random into Op.Call"
```

---

## Task 2: `ccg.shuffle` op + determinism unit test

**Files:** `mcp/modulebridge/ccg.go`, `mcp/modulebridge/ccg_test.go`.

ccg API: `(*ccg.State).Shuffle(zone ccg.ZoneName, r *core.Random) error` (ErrUnknownZone for a missing zone). `(*ccg.State).Zones[name].Members []ccg.EntityID`.

- [ ] **Step 1: Append failing tests to `mcp/modulebridge/ccg_test.go`**

```go
func TestCCG_Shuffle_Deterministic(t *testing.T) {
	build := func() *ccg.State {
		s := ccg.NewState()
		s.NewZone("deck", true)
		for i := 0; i < 8; i++ {
			id := s.NewEntity("card", "", nil)
			_ = s.MoveTo(id, "deck")
		}
		return s
	}
	order := func(s *ccg.State) []ccg.EntityID {
		return append([]ccg.EntityID(nil), s.Zones["deck"].Members...)
	}

	// Same seed -> same shuffle.
	a, b := build(), build()
	if _, err := ccgOp(t, "shuffle").Call(map[string]any{"ccg": a}, map[string]any{"zone": "deck"}, core.NewRandomFromState(seedPtr(42))); err != nil {
		t.Fatalf("shuffle a: %v", err)
	}
	if _, err := ccgOp(t, "shuffle").Call(map[string]any{"ccg": b}, map[string]any{"zone": "deck"}, core.NewRandomFromState(seedPtr(42))); err != nil {
		t.Fatalf("shuffle b: %v", err)
	}
	if !equalIDs(order(a), order(b)) {
		t.Fatalf("same seed gave different orders:\n a=%v\n b=%v", order(a), order(b))
	}
	// Different seed -> (very likely) different order; and the multiset is preserved.
	c := build()
	ccgOp(t, "shuffle").Call(map[string]any{"ccg": c}, map[string]any{"zone": "deck"}, core.NewRandomFromState(seedPtr(7)))
	if len(order(c)) != 8 {
		t.Fatalf("shuffle changed deck size: %v", order(c))
	}
}

func TestCCG_Shuffle_UnknownZone(t *testing.T) {
	s := ccg.NewState()
	if _, err := ccgOp(t, "shuffle").Call(map[string]any{"ccg": s}, map[string]any{"zone": "nope"}, core.NewRandomFromState(seedPtr(1))); err == nil {
		t.Fatal("expected ErrUnknownZone")
	}
}

func TestCCG_Shuffle_NilRNG(t *testing.T) {
	s := ccg.NewState()
	s.NewZone("deck", true)
	if _, err := ccgOp(t, "shuffle").Call(map[string]any{"ccg": s}, map[string]any{"zone": "deck"}, nil); err == nil {
		t.Fatal("expected error when rng is nil")
	}
}

func seedPtr(v uint64) *uint64 { return &v }

func equalIDs(a, b []ccg.EntityID) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
```
(Ensure `core` is imported in `ccg_test.go`; it is used here via `core.NewRandomFromState`. Add the import if missing.)

- [ ] **Step 2: Confirm failure** — `export PATH=$HOME/.local/go/bin:$PATH && cd mcp && go test ./modulebridge/ -run TestCCG_Shuffle -v` → FAIL (`shuffle` op not registered).

- [ ] **Step 3: Add the `shuffle` op to `buildCCGRegistry` (ccg.go), before `return r`**

```go
	r.Add(Op{Module: "ccg", Name: "shuffle", MCPTool: "ccg_shuffle",
		Call: func(modules map[string]any, args map[string]any, rng *core.Random) (any, error) {
			s, err := ccgFrom(modules)
			if err != nil {
				return nil, err
			}
			if rng == nil {
				return nil, fmt.Errorf("ccg.shuffle: no RNG available in this context")
			}
			zone, err := argStr(args, "zone")
			if err != nil {
				return nil, err
			}
			return nil, s.Shuffle(ccg.ZoneName(zone), rng)
		}})
```
(Add `"github.com/tjcran/boardgame-go/core"` to ccg.go's imports if Task 1 didn't already.)

- [ ] **Step 4: Confirm pass** — `... go test ./modulebridge/ -run TestCCG_Shuffle -v` → PASS (all three). Then `... go test ./modulebridge/` → ok.

- [ ] **Step 5: Commit**

```bash
cd mcp && git add modulebridge/ccg.go modulebridge/ccg_test.go
git commit -m "modulebridge: ccg.shuffle op (seeded, replay-deterministic)"
```

---

## Task 3: Integration (shuffle in a move + replay) + docs

**Files:** create `mcp/starlarkgame/modules_shuffle_test.go`; modify `mcp/docs/designed-games.md`.

- [ ] **Step 1: Write the integration test**

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

// setup builds an ordered 8-card deck. The "shuffle" move shuffles it via
// ctx.modules.ccg.shuffle. The deck order after the move must be stable
// across replay (same turn -> same ctxSeed -> same shuffle).
const shuffleSpec = `
META = {"name": "shuffler", "min_players": 1, "max_players": 1}
MODULES = ["ccg"]

def setup(ctx):
    ctx.modules.ccg.new_zone(name="deck", ordered=True)
    for i in range(8):
        c = ctx.modules.ccg.new_entity(type="card", owner="0")
        ctx.modules.ccg.move_to(entity=c, zone="deck")
    return {}

def end_if(state, ctx): return None
def legal_moves(state, ctx): return [{"name": "shuffle", "args": []}]

def shuffle(state, ctx):
    ctx.modules.ccg.shuffle(zone="deck")
    return state

MOVES = {"shuffle": {"apply": shuffle}}
`

func TestShuffle_MoveAndReplay(t *testing.T) {
	spec, err := LoadSpec(shuffleSpec)
	if err != nil {
		t.Fatalf("LoadSpec: %v", err)
	}
	if err := Validate(context.Background(), spec); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	g := BuildCoreGame(spec)
	mgr := match.NewManager(storage.NewMemory())
	mgr.MustRegister(g)
	id, _ := mgr.Create("shuffler", match.CreateOptions{NumPlayers: 1})
	jr, _ := mgr.Join(id, "p", match.JoinOptions{})

	if _, err := mgr.MoveReq(id, jr.PlayerID, jr.PlayerCredentials, core.MoveRequest{Move: "shuffle"}); err != nil {
		t.Fatalf("shuffle move: %v", err)
	}
	m, _ := mgr.State(id)
	cs := ccgStateOf(t, m.State.G.(*StarlarkG)) // helper in modules_ccg_test.go
	if cs.Size("deck") != 8 {
		t.Fatalf("deck size changed: %d", cs.Size("deck"))
	}

	// Replay re-runs setup + the shuffle move at the same turn -> identical
	// deck order -> byte-identical final G.
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

- [ ] **Step 2: Run it** — `export PATH=$HOME/.local/go/bin:$PATH && cd mcp && go test ./starlarkgame/ -run TestShuffle -v`.
  - **Compile error**: cross-check Manager/replay API against `modules_replay_test.go`; adjust. Re-run.
  - **PASS**: commit.
  - **replay mismatch**: a real determinism problem — do NOT weaken. The move's `BridgeCtx.rng` is seeded by `ctxSeed(mc.Ctx)`; replay re-runs the same move at the same turn, so the seed and shuffle must match. If they don't, check that `bridge.go` passes `c.rng.rng` (not a fresh per-call random) and that `ctxSeed` is stable. Report the orig/repl orders.

- [ ] **Step 3: Full suites** — `... go test ./starlarkgame/ ./modulebridge/` → ok / ok.

- [ ] **Step 4: Document in `mcp/docs/designed-games.md`** — in the ccg-ops area (the "Using engine modules (MODULES)" ccg paragraph or a short note near it), add `shuffle`:

```markdown
Shuffle a zone with `ctx.modules.ccg.shuffle(zone="deck")` — it uses the move's
seeded PRNG, so shuffles are replay-deterministic (a replay of the match
reproduces the same order). Note: the seed is derived per-turn, so two shuffles
of equal-size zones within the same turn draw the same sequence.
```

- [ ] **Step 5: Build + full test + commit**

```bash
export PATH=$HOME/.local/go/bin:$PATH && cd mcp && go build ./... && go test ./starlarkgame/ ./modulebridge/
```
Expected: clean + green.
```bash
cd mcp && git add starlarkgame/modules_shuffle_test.go docs/designed-games.md
git commit -m "starlarkgame: e2e ccg.shuffle determinism test + document shuffle"
```

---

## Self-Review

**Goal coverage:** Ops can reach the move's seeded PRNG (Task 1); `ccg.shuffle` exposes the first RNG-consuming op, replay-deterministic (Tasks 2-3). This closes the last cross-cutting initiative item for ccg shuffling. ✓

**Placeholder scan:** No TBD/TODO; Task 3 debug guidance is conditional with concrete checks. ✓

**Type consistency:** `Op.Call(modules map[string]any, args map[string]any, rng *core.Random)` everywhere; `ccgFrom(modules)` unchanged; `(*ccg.State).Shuffle(ccg.ZoneName, *core.Random)`; bridge passes `c.rng.rng`; tools passes a fresh `core.NewRandomFromState`. ✓

**Backward compatibility:** Task 1 adds an ignored parameter — no behavior change; all existing tests stay green (the gate). `shop.Fill`/`Roll` keep passing `nil` (they don't consume rng), unaffected. ✓

**Determinism:** Shuffle uses the same `ctxSeed`-seeded PRNG that `ctx.random` already uses; replay reproduces it. Proven in Task 3. The per-turn-repeat caveat is documented, not a correctness bug. ✓

**Scope honesty:** We did NOT register the random plugin / use `mc.Random` (nil for designed games; would add a seed-persistence problem). tabletop dice/combat is still unbridged — a future op set can now reach `rng` through the same `Op.Call` parameter with no further plumbing. ✓
```
