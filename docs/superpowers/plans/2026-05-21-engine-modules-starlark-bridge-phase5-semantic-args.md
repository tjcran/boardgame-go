# Engine Modules in Starlark — Phase 5 (semantic arg types) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give designed-game move arguments meaning beyond raw primitives — declare an arg as an `entity` (optionally constrained to a ccg zone) or a `hex` coordinate — and validate every declared move arg against its schema (and live module state) at dispatch, before the move's `apply` runs. Today `ArgsDef` is parsed but only used to render the skill skeleton; it is never enforced.

**Architecture:** No new module. Extend `ArgDef` with the semantic types and an optional `Zone`, parse them in `readArgDef`, and add a pure `validateArgs(move, args, modules)` step that `buildMovesMap` runs before `CallMove`. Primitive types (`int`/`string`/`bool`, with `min`/`max`) are checked structurally; `entity` checks the arg is a valid entity token and — when a `zone` is given and the `ccg` module is present — that the entity is currently in that zone; `hex` checks the arg is a 2-int `[x, y]` pair. The skill-skeleton renderer surfaces the richer types so the LLM/UI understands what each arg means.

**Tech Stack:** Go, `go.starlark.net`, the `mcp/starlarkgame` adapter, `modules/ccg` + `mcp/modulebridge` (for entity-token parsing / zone membership).

**Reference spec:** `docs/superpowers/specs/2026-05-21-engine-modules-in-starlark-and-mcp.md` (phase 5; "Args schema extended with semantic types … the engine validates these against module state before apply runs").

**Scope note:** HOOKS (event-bus handlers) is explicitly NOT in this plan — it is deferred to its own phase 6 due to re-entrant-handler / determinism complexity. Semantic types covered here: `entity` (+optional `zone`) and `hex`, mapping to the shipped `ccg` and `tabletop` modules. `target` is not an arg type — target selection is the phase-4 block/resume flow.

**Current behavior to preserve:**
- `ArgsDef` is parsed by `readArgDef` (`spec.go`) into `Move.ArgsDef []ArgDef` where `ArgDef{Name, Type string; Min, Max *int64}`. Moves with NO declared args (`ArgsDef` empty) stay unconstrained — validation is a no-op for them, so existing arg-less specs are unaffected.
- `renderArgSig` (`skill_skeleton.go`) renders `name: type [min..max]`.
- `buildMovesMap` (`game.go`) builds the per-move `core.MoveFn` closure: asserts `*StarlarkG`, builds `BridgeCtx`, calls `s.CallMove(...)`. Validation is inserted here, before `CallMove`.

---

## File Structure

- `mcp/starlarkgame/spec.go` (modify) — add `Zone string` to `ArgDef`; parse `zone` in `readArgDef`.
- `mcp/starlarkgame/argvalidate.go` (new) — `validateArgs(mv Move, args []any, modules map[string]any) error` and `validateArg`.
- `mcp/starlarkgame/argvalidate_test.go` (new) — unit tests for each type.
- `mcp/starlarkgame/game.go` (modify) — `buildMovesMap` captures `mv.ArgsDef` and runs `validateArgs` before `CallMove`.
- `mcp/starlarkgame/modules_argvalidate_test.go` (new) — integration: entity-in-zone arg rejected when out of zone, accepted when in zone.
- `mcp/starlarkgame/skill_skeleton.go` (modify) — `renderArgSig` shows `entity(zone)` / `hex`.
- `mcp/docs/designed-games.md` (modify) — document semantic arg types.

---

## Task 1: `ArgDef.Zone` + the pure validators

**Files:** `mcp/starlarkgame/spec.go`, create `mcp/starlarkgame/argvalidate.go`, create `mcp/starlarkgame/argvalidate_test.go`.

- [ ] **Step 1: Write `mcp/starlarkgame/argvalidate_test.go`**

```go
package starlarkgame

import (
	"testing"

	"github.com/tjcran/boardgame-go/modules/ccg"
)

func TestValidateArgs_PrimitivesAndCount(t *testing.T) {
	mv := Move{Name: "m", ArgsDef: []ArgDef{{Name: "n", Type: "int"}, {Name: "s", Type: "string"}}}
	if err := validateArgs(mv, []any{int64(3), "x"}, nil); err != nil {
		t.Fatalf("valid: %v", err)
	}
	if err := validateArgs(mv, []any{int64(3)}, nil); err == nil {
		t.Fatal("expected arg-count mismatch error")
	}
	if err := validateArgs(mv, []any{"notint", "x"}, nil); err == nil {
		t.Fatal("expected int type error")
	}
}

func TestValidateArgs_NoDefIsUnconstrained(t *testing.T) {
	mv := Move{Name: "m"} // no ArgsDef
	if err := validateArgs(mv, []any{int64(1), "a", true}, nil); err != nil {
		t.Fatalf("argless move should be unconstrained: %v", err)
	}
}

func TestValidateArgs_IntMinMax(t *testing.T) {
	lo, hi := int64(1), int64(5)
	mv := Move{Name: "m", ArgsDef: []ArgDef{{Name: "n", Type: "int", Min: &lo, Max: &hi}}}
	if err := validateArgs(mv, []any{int64(3)}, nil); err != nil {
		t.Fatalf("in range: %v", err)
	}
	if err := validateArgs(mv, []any{int64(9)}, nil); err == nil {
		t.Fatal("expected out-of-range error")
	}
}

func TestValidateArgs_EntityZone(t *testing.T) {
	st := ccg.NewState()
	st.NewZone("hand", false)
	st.NewZone("deck", true)
	id := st.NewEntity("card", "0", nil) // ent:1
	st.MoveTo(id, "deck")
	mods := map[string]any{"ccg": st}

	mv := Move{Name: "play", ArgsDef: []ArgDef{{Name: "c", Type: "entity", Zone: "hand"}}}
	// ent:1 is in deck, not hand → reject.
	if err := validateArgs(mv, []any{"ent:1"}, mods); err == nil {
		t.Fatal("expected reject: entity not in zone hand")
	}
	// Move it to hand → accept.
	st.MoveTo(id, "hand")
	if err := validateArgs(mv, []any{"ent:1"}, mods); err != nil {
		t.Fatalf("entity in hand should pass: %v", err)
	}
	// Garbage token → reject regardless of zone.
	if err := validateArgs(mv, []any{"nope"}, mods); err == nil {
		t.Fatal("expected reject: bad entity token")
	}
}

func TestValidateArgs_Hex(t *testing.T) {
	mv := Move{Name: "move", ArgsDef: []ArgDef{{Name: "to", Type: "hex"}}}
	if err := validateArgs(mv, []any{[]any{int64(2), int64(3)}}, nil); err != nil {
		t.Fatalf("valid hex: %v", err)
	}
	if err := validateArgs(mv, []any{[]any{int64(2)}}, nil); err == nil {
		t.Fatal("expected reject: hex needs 2 ints")
	}
	if err := validateArgs(mv, []any{"2,3"}, nil); err == nil {
		t.Fatal("expected reject: hex must be a list")
	}
}
```

- [ ] **Step 2: Confirm failure** — `export PATH=$HOME/.local/go/bin:$PATH && cd mcp && go test ./starlarkgame/ -run TestValidateArgs -v` → FAIL (`validateArgs` / `ArgDef.Zone` undefined).

- [ ] **Step 3: Add `Zone` to `ArgDef` and parse it (spec.go)**

In the `ArgDef` struct add:
```go
	// Zone, for Type=="entity", optionally constrains the entity to be in
	// this ccg zone at dispatch time. Empty means any registered entity.
	Zone string
```
In `readArgDef`, after `a := ArgDef{Name: name, Type: typ}`, add:
```go
	if z, ok := pickString("zone"); ok {
		a.Zone = z
	}
```
(`pickString` returns `(string, bool)` and already exists in `readArgDef`.)

- [ ] **Step 4: Write `mcp/starlarkgame/argvalidate.go`**

```go
package starlarkgame

import (
	"fmt"

	"github.com/tjcran/boardgame-go/modules/ccg"
	"github.com/tjcran/boardgame-go/mcp/modulebridge"
)

// validateArgs checks a move's actual args against its declared ArgsDef.
// A move with no ArgsDef is unconstrained (returns nil). When ArgsDef is
// present, the arg count must match and each arg must satisfy its
// declared type. Semantic types (entity, hex) are validated against
// live module state where applicable.
func validateArgs(mv Move, args []any, modules map[string]any) error {
	if len(mv.ArgsDef) == 0 {
		return nil
	}
	if len(args) != len(mv.ArgsDef) {
		return fmt.Errorf("move %q expects %d args, got %d", mv.Name, len(mv.ArgsDef), len(args))
	}
	for i, ad := range mv.ArgsDef {
		if err := validateArg(ad, args[i], modules); err != nil {
			return fmt.Errorf("arg %q: %w", ad.Name, err)
		}
	}
	return nil
}

func validateArg(ad ArgDef, v any, modules map[string]any) error {
	switch ad.Type {
	case "", "any":
		return nil
	case "int":
		n, ok := v.(int64)
		if !ok {
			return fmt.Errorf("must be an int, got %T", v)
		}
		if ad.Min != nil && n < *ad.Min {
			return fmt.Errorf("must be >= %d", *ad.Min)
		}
		if ad.Max != nil && n > *ad.Max {
			return fmt.Errorf("must be <= %d", *ad.Max)
		}
		return nil
	case "string":
		if _, ok := v.(string); !ok {
			return fmt.Errorf("must be a string, got %T", v)
		}
		return nil
	case "bool":
		if _, ok := v.(bool); !ok {
			return fmt.Errorf("must be a bool, got %T", v)
		}
		return nil
	case "entity":
		tok, ok := v.(string)
		if !ok {
			return fmt.Errorf("must be an entity token string, got %T", v)
		}
		id, err := modulebridge.ParseEntityToken(tok)
		if err != nil {
			return err
		}
		if ad.Zone != "" {
			s, ok := modules["ccg"].(*ccg.State)
			if !ok {
				return fmt.Errorf("entity zone constraint needs the ccg module")
			}
			if !s.Contains(ccg.ZoneName(ad.Zone), id) {
				return fmt.Errorf("entity %s is not in zone %q", tok, ad.Zone)
			}
		}
		return nil
	case "hex":
		lst, ok := v.([]any)
		if !ok || len(lst) != 2 {
			return fmt.Errorf("must be a [x, y] pair")
		}
		for _, c := range lst {
			if _, ok := c.(int64); !ok {
				return fmt.Errorf("hex coordinates must be ints")
			}
		}
		return nil
	default:
		return fmt.Errorf("unknown arg type %q", ad.Type)
	}
}
```

- [ ] **Step 5: Confirm pass** — `... go test ./starlarkgame/ -run TestValidateArgs -v` → PASS (all five).

- [ ] **Step 6: Full starlarkgame suite** — `export PATH=$HOME/.local/go/bin:$PATH && cd mcp && go test ./starlarkgame/` → ok (no regressions; existing specs that declare arg types with matching counts still pass).

- [ ] **Step 7: Commit**

```bash
cd mcp && git add starlarkgame/spec.go starlarkgame/argvalidate.go starlarkgame/argvalidate_test.go
git commit -m "starlarkgame: ArgDef.Zone + validateArgs for primitive/entity/hex arg types"
```

---

## Task 2: Enforce validation at dispatch + integration test

**Files:** `mcp/starlarkgame/game.go`, create `mcp/starlarkgame/modules_argvalidate_test.go`.

- [ ] **Step 1: Write the failing integration test `mcp/starlarkgame/modules_argvalidate_test.go`**

```go
package starlarkgame

import (
	"context"
	"testing"

	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/match"
	"github.com/tjcran/boardgame-go/storage"
)

// play_card declares its arg as an entity that must be in the player's hand.
// Setup puts ent:1 in deck and ent:2 in hand. Playing ent:1 (in deck) must be
// rejected by arg validation; playing ent:2 (in hand) succeeds.
const handArgSpec = `
META = {"name": "handarg", "min_players": 1, "max_players": 1}
MODULES = ["ccg"]

def setup(ctx):
    ctx.modules.ccg.new_zone(name="deck", ordered=True)
    ctx.modules.ccg.new_zone(name="hand", ordered=False)
    ctx.modules.ccg.new_zone(name="board", ordered=False)
    a = ctx.modules.ccg.new_entity(type="card", owner="0")  # ent:1
    b = ctx.modules.ccg.new_entity(type="card", owner="0")  # ent:2
    ctx.modules.ccg.move_to(entity=a, zone="deck")
    ctx.modules.ccg.move_to(entity=b, zone="hand")
    return {}

def end_if(state, ctx): return None
def legal_moves(state, ctx): return []

def play_card(state, ctx, card):
    ctx.modules.ccg.move_to(entity=card, zone="board")
    return state

MOVES = {"play_card": {"apply": play_card, "args": [{"name": "card", "type": "entity", "zone": "hand"}]}}
`

func TestHandArg_RejectsOutOfZoneEntity(t *testing.T) {
	spec, err := LoadSpec(handArgSpec)
	if err != nil {
		t.Fatalf("LoadSpec: %v", err)
	}
	if err := Validate(context.Background(), spec); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	g := BuildCoreGame(spec)
	mgr := match.NewManager(storage.NewMemory())
	mgr.MustRegister(g)
	id, _ := mgr.Create("handarg", match.CreateOptions{NumPlayers: 1})
	jr, _ := mgr.Join(id, "p", match.JoinOptions{})

	// ent:1 is in deck, not hand → move rejected by arg validation.
	if _, err := mgr.MoveReq(id, jr.PlayerID, jr.PlayerCredentials, core.MoveRequest{Move: "play_card", Args: []any{"ent:1"}}); err == nil {
		t.Fatal("expected play_card(ent:1) to be rejected (not in hand)")
	}

	// ent:2 is in hand → succeeds.
	if _, err := mgr.MoveReq(id, jr.PlayerID, jr.PlayerCredentials, core.MoveRequest{Move: "play_card", Args: []any{"ent:2"}}); err != nil {
		t.Fatalf("play_card(ent:2) should succeed: %v", err)
	}
}
```

- [ ] **Step 2: Confirm it fails** — `export PATH=$HOME/.local/go/bin:$PATH && cd mcp && go test ./starlarkgame/ -run TestHandArg -v`. Expected: FAIL — without dispatch validation, `play_card(ent:1)` is NOT rejected (the move just moves ent:1 deck→board and returns nil error), so the first assertion fails.

- [ ] **Step 3: Wire validation into `buildMovesMap` (game.go)**

In `buildMovesMap`, capture the move's ArgsDef alongside the existing captures and validate before `CallMove`. The loop currently does `for name, mv := range src { name := name; endsTurn := mv.EndsTurn; ... }`. Add a capture of the whole move (for `validateArgs`) and the validation call inside the closure, before `s.CallMove`:

```go
	for name, mv := range src {
		name := name
		mv := mv // capture for validateArgs
		endsTurn := mv.EndsTurn
		out[name] = core.MoveFn(func(mc *core.MoveContext, args ...any) (core.G, error) {
			sg, ok := mc.G.(*StarlarkG)
			if !ok {
				return nil, fmt.Errorf("starlarkgame: state is not *StarlarkG")
			}
			if err := validateArgs(mv, args, sg.Modules); err != nil {
				return nil, err
			}
			bc := &BridgeCtx{
				// … unchanged …
			}
			// … unchanged: AttachSeededRandom, CallMove, endsTurn handling …
		})
	}
```
Make the minimal edit: add `mv := mv` capture and the `validateArgs` block right after the `*StarlarkG` assertion. Leave everything else intact.

- [ ] **Step 4: Confirm pass** — `... go test ./starlarkgame/ -run TestHandArg -v` → PASS.

- [ ] **Step 5: Full suites (no regressions)** — `export PATH=$HOME/.local/go/bin:$PATH && cd mcp && go test ./starlarkgame/ ./modulebridge/` → ok / ok. (The phase-4 bolt spec declares `{"name":"target","type":"string"}` and calls with one string → still valid. The mini specs are arg-less → unconstrained.)

- [ ] **Step 6: Commit**

```bash
cd mcp && git add starlarkgame/game.go starlarkgame/modules_argvalidate_test.go
git commit -m "starlarkgame: enforce declared arg schema at move dispatch"
```

---

## Task 3: Skeleton rendering + docs

**Files:** `mcp/starlarkgame/skill_skeleton.go`, `mcp/docs/designed-games.md`.

- [ ] **Step 1: Extend `renderArgSig` to show semantic types (skill_skeleton.go)**

Update `renderArgSig` so an `entity` arg with a zone renders as `name: entity(zone)`:
```go
func renderArgSig(a ArgDef) string {
	out := a.Name
	if a.Type != "" {
		if a.Type == "entity" && a.Zone != "" {
			out += ": entity(" + a.Zone + ")"
		} else {
			out += ": " + a.Type
		}
	}
	if a.Min != nil && a.Max != nil {
		out += fmt.Sprintf(" [%d..%d]", *a.Min, *a.Max)
	} else if a.Min != nil {
		out += fmt.Sprintf(" [≥%d]", *a.Min)
	} else if a.Max != nil {
		out += fmt.Sprintf(" [≤%d]", *a.Max)
	}
	return out
}
```

- [ ] **Step 2: Add a unit test for the renderer** (append to an existing skill-skeleton test file, e.g. `skill_skeleton_test.go`, or add to `argvalidate_test.go`):
```go
func TestRenderArgSig_EntityZone(t *testing.T) {
	if got := renderArgSig(ArgDef{Name: "card", Type: "entity", Zone: "hand"}); got != "card: entity(hand)" {
		t.Fatalf("got %q, want card: entity(hand)", got)
	}
	if got := renderArgSig(ArgDef{Name: "c", Type: "entity"}); got != "c: entity" {
		t.Fatalf("got %q, want c: entity", got)
	}
}
```
Run: `export PATH=$HOME/.local/go/bin:$PATH && cd mcp && go test ./starlarkgame/ -run TestRenderArgSig -v` → PASS.

- [ ] **Step 3: Document in `mcp/docs/designed-games.md`**

Find the section listing move/args conventions (search for `"args"` or the engine-limits text). Add a "Semantic argument types" subsection:

```markdown
## Semantic argument types

Move `args` entries declare a `type`, validated at dispatch before `apply` runs.
Beyond `int` (with optional `min`/`max`), `string`, and `bool`, two semantic
types tie an arg to engine state:

- `{"name": "card", "type": "entity", "zone": "hand"}` — the arg must be a ccg
  entity token (e.g. `"ent:7"`); with `zone` set, the entity must currently be
  in that ccg zone, else the move is rejected. Omit `zone` to accept any entity.
- `{"name": "to", "type": "hex"}` — the arg must be a `[x, y]` integer pair.

A move with no declared `args` is unconstrained. When `args` are declared, the
arg count must match and each arg must satisfy its type — invalid moves are
rejected before `apply`, so handlers can trust their inputs.
```

- [ ] **Step 4: Build + full test + commit**

```bash
export PATH=$HOME/.local/go/bin:$PATH && cd mcp && go build ./... && go test ./starlarkgame/ ./modulebridge/
```
Expected: clean + green.
```bash
cd mcp && git add starlarkgame/skill_skeleton.go starlarkgame/skill_skeleton_test.go starlarkgame/argvalidate_test.go docs/designed-games.md
git commit -m "starlarkgame: render semantic arg types in skill skeleton + document"
```
(Adjust the `git add` to whichever test file you put `TestRenderArgSig_EntityZone` in.)

---

## Self-Review

**Spec coverage:** Semantic arg types (`entity`+`zone`, `hex`) parsed, validated against live module state at dispatch, and surfaced in the skeleton — matching the spec's "engine validates these against module state before apply runs." HOOKS correctly deferred to phase 6. ✓

**Placeholder scan:** No TBD/TODO; the only conditional guidance (Task 3 `git add` file choice) names the exact options. ✓

**Type consistency:** `ArgDef.Zone string`; `validateArgs(mv Move, args []any, modules map[string]any) error` / `validateArg(ad ArgDef, v any, modules map[string]any) error`; `modulebridge.ParseEntityToken`, `ccg.State.Contains(ccg.ZoneName, ccg.EntityID)`. `ToGo` produces `int64` for ints and `[]any` for lists, which the validators assert. ✓

**Backward compatibility:** Arg-less moves and moves whose declared types match their existing call shape are unaffected (validation only bites when a declared type/zone/count is actually violated). The phase-1–4 integration specs were checked: mini-ccg/tabletop/shop moves are arg-less or use `string` args correctly; the bolt resume arg is `string`. ✓

**Determinism:** Validation is a pure function of (args, declared schema, current module state); it rejects or passes deterministically and runs identically under replay. No RNG. ✓

**Known caveat (carry forward):** `hex` validates shape only (2 ints), not board bounds — bounds depend on a tabletop board that may not exist at validate time; out-of-bounds is the move's own concern. The remaining initiative item is phase 6 (HOOKS event bus) plus the still-unsolved seeded-RNG-to-ops threading.
```
