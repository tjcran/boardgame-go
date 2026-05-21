# Engine Modules in Starlark — Phase 4 (TargetRequest block/resume) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let Starlark-authored games pause a move to ask a player to pick targets and resume on the player's selection — `core.TargetRequest` via the engine's `Queue.RequestTarget` block/resume protocol — exposed as `ctx.request_target(...)`, `ctx.resuming_target()`, `ctx.validate_selection(...)`, plus a `resumeTag` field on the `make_move` MCP tool so resume moves can be dispatched.

**Architecture:** This is NOT a module binding (no `modulebridge` entry). It is a core-protocol binding on `ctx`, like the existing `ctx.events`. A move's `apply` calls `ctx.request_target(kind, candidates, …)`, which calls `mc.Queue.RequestTarget(playerID, core.TargetRequest{…})` — pausing the cascade (the engine persists with a pending `BlockSpec` in `State.Blocks`). The move ends; the player/Claude later dispatches a **resume move** via `make_move(..., resumeTag=kind, args=[selection])`. The reducer matches the tag against `State.Blocks`, removes it, and sets `mc.ResumingBlock` on the resume move's context. That move's `apply` reads `ctx.resuming_target()` and `ctx.validate_selection([...])`, then applies the choice. Threading is move-only: `BridgeCtx` gains `Queue *core.Queue` and `ResumingBlock *core.BlockSpec`, set in `buildMovesMap` from `mc.Queue` / `mc.ResumingBlock` (parallel to how `Events` is threaded).

**Tech Stack:** Go, `go.starlark.net`, `core` (`Queue.RequestTarget`, `TargetRequest`, `ValidateSelection`, `MoveContext.ResumingBlock`, `MoveRequest.ResumeTag`), the `mcp/starlarkgame` adapter, `match.Manager`.

**Reference spec:** `docs/superpowers/specs/2026-05-21-engine-modules-in-starlark-and-mcp.md` (phase 4; the "TargetRequest (mid-move selection) — block/resume pair" data-flow section).

**Key engine facts (verified against source):**
- `core.MoveContext` has `Queue *Queue` and `ResumingBlock *BlockSpec` (nil for non-resume moves).
- `(*core.Queue).RequestTarget(playerID string, req core.TargetRequest)` pauses the cascade; `Tag` is set to `req.Kind`. `(*core.Queue).PendingBlocks() []BlockSpec` (exported, for tests).
- `core.TargetRequest{Kind string; Candidates []any; Min, Max int; Source any; Data map[string]any}`.
- `core.ValidateSelection(req TargetRequest, sel []any) error` — checks cardinality (Min/Max; Min 0→1, Max 0→Min) and membership (DeepEqual with int/float64 normalization; empty Candidates = open-ended). Sentinels `core.ErrTargetCardinality`, `core.ErrTargetNotCandidate`.
- `core.MoveRequest.ResumeTag string` — when set, the reducer resolves it against `State.Blocks`; mismatch → `core.ErrUnknownResumeTag`; pending blocks with no ResumeTag on a normal move → `core.ErrBlocked`.
- `core.State.Blocks []BlockSpec` is serialized (`json:"blocks"`), so a `MakeMoveResult.State` exposes pending requests (clients read `state.blocks[].target` to know candidates).
- `BlockSpec.Target` candidates round-trip through JSON between the requesting and resuming move; numbers become float64 — `ValidateSelection` normalizes, and entity tokens are strings (compare exactly). So both numeric and token candidates work across the pause.

**Scope note:** Threading multiple concurrent requests of the same Kind is out of scope (the engine supports it via distinct tags / Data discriminators; the binding exposes the common single-request case). `legal_moves`/`Enumerate` behavior while blocked is unchanged (UI concern) — the integration test dispatches the resume move directly with `resumeTag`.

---

## File Structure

- `mcp/starlarkgame/bridge.go` (modify) — `BridgeCtx` gains `Queue`/`ResumingBlock`; `asStarlark` exposes `request_target` (when `Queue != nil`) and `resuming_target` + `validate_selection` (when `ResumingBlock != nil`). New helper methods mirror `eventsAsStarlark`.
- `mcp/starlarkgame/game.go` (modify) — `buildMovesMap` threads `mc.Queue` and `mc.ResumingBlock` into the move's `BridgeCtx`.
- `mcp/starlarkgame/targetrequest_test.go` (new) — bridge-level tests via `CallMove`.
- `mcp/tools.go` (modify) — `MakeMoveArgs` gains `ResumeTag`; `MakeMove` threads it into `core.MoveRequest`.
- `mcp/register.go` (modify) — add `resumeTag` to the `make_move` tool input schema.
- `mcp/starlarkgame/targetrequest_integration_test.go` (new) — bolt→resolve spec through `match.Manager` + replay.
- `mcp/docs/designed-games.md` (modify) — document target selection.

---

## Task 1: `ctx.request_target` / `resuming_target` / `validate_selection` bindings

**Files:** `mcp/starlarkgame/bridge.go`, `mcp/starlarkgame/game.go`, `mcp/starlarkgame/targetrequest_test.go`.

- [ ] **Step 1: Write the failing tests `mcp/starlarkgame/targetrequest_test.go`**

```go
package starlarkgame

import (
	"context"
	"testing"

	"github.com/tjcran/boardgame-go/core"
)

// A move that requests a target pushes a typed block on the Queue.
func TestRequestTarget_PushesBlock(t *testing.T) {
	spec, err := LoadSpec(`
META = {"name":"t","min_players":1,"max_players":2}
def setup(ctx): return {}
def end_if(state, ctx): return None
def legal_moves(state, ctx): return []
def cast(state, ctx):
    ctx.request_target(kind="creature", candidates=["ent:1", "ent:2"], min=1, max=1)
    return state
MOVES = {"cast": {"apply": cast, "ends_turn": False}}
`)
	if err != nil {
		t.Fatalf("LoadSpec: %v", err)
	}
	q := &core.Queue{}
	bc := &BridgeCtx{PlayerID: "0", Queue: q}
	if _, err := spec.CallMove(context.Background(), bc, "cast", map[string]any{}, nil); err != nil {
		t.Fatalf("CallMove: %v", err)
	}
	blocks := q.PendingBlocks()
	if len(blocks) != 1 {
		t.Fatalf("got %d blocks, want 1", len(blocks))
	}
	b := blocks[0]
	if b.Tag != "creature" || b.PlayerID != "0" || b.Target == nil {
		t.Fatalf("block wrong: %#v", b)
	}
	if b.Target.Kind != "creature" || len(b.Target.Candidates) != 2 || b.Target.Min != 1 || b.Target.Max != 1 {
		t.Fatalf("target wrong: %#v", b.Target)
	}
}

// A resume move reads the request via resuming_target and validates the
// selection; an in-set selection passes, an out-of-set one rejects.
func TestResumingTargetAndValidate(t *testing.T) {
	spec, err := LoadSpec(`
META = {"name":"t","min_players":1,"max_players":2}
def setup(ctx): return {}
def end_if(state, ctx): return None
def legal_moves(state, ctx): return []
def resolve(state, ctx, choice):
    req = ctx.resuming_target()
    ctx.validate_selection([choice])
    return {"kind": req["kind"], "picked": choice}
MOVES = {"resolve": {"apply": resolve, "args": [{"name":"choice","type":"string"}]}}
`)
	if err != nil {
		t.Fatalf("LoadSpec: %v", err)
	}
	rb := &core.BlockSpec{
		Tag:      "creature",
		PlayerID: "0",
		Target:   &core.TargetRequest{Kind: "creature", Candidates: []any{"ent:1", "ent:2"}, Min: 1, Max: 1},
	}
	bc := &BridgeCtx{PlayerID: "0", ResumingBlock: rb}

	// Valid selection: ent:1 is a candidate.
	got, err := spec.CallMove(context.Background(), bc, "resolve", map[string]any{}, []any{"ent:1"})
	if err != nil {
		t.Fatalf("valid resolve: %v", err)
	}
	if got["kind"] != "creature" || got["picked"] != "ent:1" {
		t.Fatalf("resolve result wrong: %#v", got)
	}

	// Invalid selection: ent:9 not in candidate set → move rejected.
	if _, err := spec.CallMove(context.Background(), bc, "resolve", map[string]any{}, []any{"ent:9"}); err == nil {
		t.Fatal("expected validate_selection to reject ent:9")
	}
}
```

- [ ] **Step 2: Run to confirm failure**

Run: `export PATH=$HOME/.local/go/bin:$PATH && cd mcp && go test ./starlarkgame/ -run 'TestRequestTarget|TestResumingTarget' -v`
Expected: FAIL — `ctx` has no `.request_target` / `.resuming_target` / `.validate_selection`.

- [ ] **Step 3: Add fields to `BridgeCtx` and wire `asStarlark` in `bridge.go`**

Add to the `BridgeCtx` struct (near `Events`):
```go
	// Queue, when set, exposes ctx.request_target to pause a move for a
	// player target selection. Set only for move applies (nil for
	// setup/end_if/legal_moves/player_view).
	Queue *core.Queue

	// ResumingBlock, when set, is the block this move resumed — exposes
	// ctx.resuming_target() and ctx.validate_selection(). Set only when a
	// move was dispatched with MoveRequest.ResumeTag matching a block.
	ResumingBlock *core.BlockSpec
```

In `asStarlark`, after the `Events` block, add:
```go
	if c.Queue != nil {
		attrs["request_target"] = c.requestTargetBuiltin()
	}
	if c.ResumingBlock != nil {
		attrs["resuming_target"] = c.resumingTargetBuiltin()
		attrs["validate_selection"] = c.validateSelectionBuiltin()
	}
```

Add the three builtin constructors (mirror `eventsAsStarlark`'s style; `core`, `fmt`, `starlark` are imported — verify):
```go
func (c *BridgeCtx) requestTargetBuiltin() starlark.Value {
	return starlark.NewBuiltin("request_target",
		func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
			if len(args) != 0 {
				return nil, fmt.Errorf("request_target: use keyword args only")
			}
			req := core.TargetRequest{}
			for _, kv := range kwargs {
				k, ok := kv[0].(starlark.String)
				if !ok {
					return nil, fmt.Errorf("request_target: non-string kwarg")
				}
				gv, err := ToGo(kv[1])
				if err != nil {
					return nil, err
				}
				switch string(k) {
				case "kind":
					s, ok := gv.(string)
					if !ok {
						return nil, fmt.Errorf("request_target: kind must be a string")
					}
					req.Kind = s
				case "candidates":
					lst, ok := gv.([]any)
					if !ok {
						return nil, fmt.Errorf("request_target: candidates must be a list")
					}
					req.Candidates = lst
				case "min":
					n, ok := gv.(int64)
					if !ok {
						return nil, fmt.Errorf("request_target: min must be an int")
					}
					req.Min = int(n)
				case "max":
					n, ok := gv.(int64)
					if !ok {
						return nil, fmt.Errorf("request_target: max must be an int")
					}
					req.Max = int(n)
				case "source":
					req.Source = gv
				case "data":
					if m, ok := gv.(map[string]any); ok {
						req.Data = m
					}
				default:
					return nil, fmt.Errorf("request_target: unknown arg %q", string(k))
				}
			}
			if req.Kind == "" {
				return nil, fmt.Errorf("request_target: kind is required")
			}
			c.Queue.RequestTarget(c.PlayerID, req)
			return starlark.None, nil
		})
}

func (c *BridgeCtx) resumingTargetBuiltin() starlark.Value {
	return starlark.NewBuiltin("resuming_target",
		func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
			if len(args) != 0 {
				return nil, fmt.Errorf("resuming_target: takes no args")
			}
			if c.ResumingBlock == nil || c.ResumingBlock.Target == nil {
				return starlark.None, nil
			}
			tr := c.ResumingBlock.Target
			out := map[string]any{
				"kind":       tr.Kind,
				"candidates": tr.Candidates,
				"min":        int64(tr.Min),
				"max":        int64(tr.Max),
			}
			if tr.Source != nil {
				out["source"] = tr.Source
			}
			if tr.Data != nil {
				out["data"] = map[string]any(tr.Data)
			}
			return ToStarlark(out)
		})
}

func (c *BridgeCtx) validateSelectionBuiltin() starlark.Value {
	return starlark.NewBuiltin("validate_selection",
		func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("validate_selection: expected 1 arg (the selection list)")
			}
			if c.ResumingBlock == nil || c.ResumingBlock.Target == nil {
				return nil, fmt.Errorf("validate_selection: no resuming target request")
			}
			gv, err := ToGo(args[0])
			if err != nil {
				return nil, err
			}
			lst, ok := gv.([]any)
			if !ok {
				return nil, fmt.Errorf("validate_selection: arg must be a list")
			}
			if err := core.ValidateSelection(*c.ResumingBlock.Target, lst); err != nil {
				return nil, err
			}
			return starlark.None, nil
		})
}
```

> Note on `ToGo` numeric type: `ToGo` converts Starlark ints to `int64`. The `min`/`max` cases assert `int64` accordingly. Candidates from `ToStarlark(tr.Candidates)` where Candidates came across JSON as `float64` will surface to Starlark as floats — that's fine for display; `validate_selection` re-converts via `ToGo` and `core.ValidateSelection` normalizes int/float64.

- [ ] **Step 4: Thread `Queue` + `ResumingBlock` in `game.go` `buildMovesMap`**

In the `core.MoveFn` closure inside `buildMovesMap`, add the two fields to the `BridgeCtx` literal (alongside `Events: mc.Events`):
```go
				bc := &BridgeCtx{
					NumPlayers:    mc.Ctx.NumPlayers,
					PlayerID:      mc.PlayerID,
					Phase:         mc.Ctx.Phase,
					Events:        mc.Events,
					Modules:       sg.Modules,
					Queue:         mc.Queue,
					ResumingBlock: mc.ResumingBlock,
				}
```
(Keep the existing `bc.AttachSeededRandom(ctxSeed(mc.Ctx))` line and the rest unchanged.)

- [ ] **Step 5: Run to confirm pass**

Run: `export PATH=$HOME/.local/go/bin:$PATH && cd mcp && go test ./starlarkgame/ -run 'TestRequestTarget|TestResumingTarget' -v`
Expected: PASS (both tests).

- [ ] **Step 6: Full starlarkgame suite (no regressions)**

Run: `export PATH=$HOME/.local/go/bin:$PATH && cd mcp && go test ./starlarkgame/`
Expected: ok.

- [ ] **Step 7: Commit**

```bash
cd mcp && git add starlarkgame/bridge.go starlarkgame/game.go starlarkgame/targetrequest_test.go
git commit -m "starlarkgame: ctx.request_target/resuming_target/validate_selection bindings"
```

---

## Task 2: `resumeTag` on the `make_move` MCP tool

**Files:** `mcp/tools.go`, `mcp/register.go`.

- [ ] **Step 1: Add `ResumeTag` to `MakeMoveArgs` and thread it (tools.go)**

Add the field to `MakeMoveArgs`:
```go
	ResumeTag   string `json:"resumeTag,omitempty"`
```
In `MakeMove`, add it to the `core.MoveRequest` literal:
```go
	state, err := t.Manager.MoveReqCtx(ctx, args.MatchID, args.PlayerID, args.Credentials, core.MoveRequest{
		PlayerID:  args.PlayerID,
		Move:      args.Move,
		Args:      args.Args,
		ResumeTag: args.ResumeTag,
	})
```

- [ ] **Step 2: Add `resumeTag` to the `make_move` input schema (register.go)**

`grep -n "make_move" mcp/register.go` to find the tool registration, then add a `resumeTag` property to its `InputSchema` JSON (a string, optional — do NOT add to `required`). Match the existing property style. Example property to insert alongside the others:
```json
"resumeTag": {"type": "string", "description": "Set to a pending block's tag (its TargetRequest kind) to resume a paused move with this move as the selection."}
```
If the schema sets `"additionalProperties": false`, this addition is REQUIRED for resume calls to pass schema validation — verify it's present.

- [ ] **Step 3: Build + existing make_move tests still pass**

Run: `export PATH=$HOME/.local/go/bin:$PATH && cd mcp && go build ./... && go test . -run 'TestMakeMove|MakeMove' 2>&1 | tail -20`
Expected: build clean; any existing make_move tests pass (the two pre-existing unrelated `mcp` failures, `TestStarlarkGameMoves_WithSQLitePersistence` and `TestImplementAKnownGamePromptShape`, remain — ignore them).

- [ ] **Step 4: Commit**

```bash
cd mcp && git add tools.go register.go
git commit -m "mcp: make_move accepts resumeTag to dispatch resume moves"
```

---

## Task 3: End-to-end block/resume + replay integration

**Files:** `mcp/starlarkgame/targetrequest_integration_test.go`.

A 2-player spec: P0's `cast_bolt` requests a target among P1's two creatures (does NOT end turn). The resume move `resolve_bolt` (resumeTag `creature`) reads the request, validates the selection, and "kills" the chosen creature (records it in state). Verify: after cast, `State.Blocks` has the request; a non-resume move is blocked; the resume move with `resumeTag` applies; replay is byte-identical.

- [ ] **Step 1: Write `mcp/starlarkgame/targetrequest_integration_test.go`**

```go
package starlarkgame

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/match"
	"github.com/tjcran/boardgame-go/storage"
)

const boltSpec = `
META = {"name": "bolt", "min_players": 2, "max_players": 2}

def setup(ctx):
    return {"creatures": ["ent:1", "ent:2"], "dead": []}

def end_if(state, ctx): return None
def legal_moves(state, ctx): return []

def cast_bolt(state, ctx):
    ctx.request_target(kind="creature", candidates=state["creatures"], min=1, max=1)
    return state

def resolve_bolt(state, ctx, target):
    ctx.validate_selection([target])
    dead = list(state["dead"])
    dead.append(target)
    remaining = [c for c in state["creatures"] if c != target]
    return {"creatures": remaining, "dead": dead}

MOVES = {
    "cast_bolt": {"apply": cast_bolt, "ends_turn": False},
    "resolve_bolt": {"apply": resolve_bolt, "ends_turn": False, "args": [{"name": "target", "type": "string"}]},
}
`

func TestBolt_RequestResolveReplay(t *testing.T) {
	spec, err := LoadSpec(boltSpec)
	if err != nil {
		t.Fatalf("LoadSpec: %v", err)
	}
	if err := Validate(context.Background(), spec); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	g := BuildCoreGame(spec)

	mgr := match.NewManager(storage.NewMemory())
	mgr.MustRegister(g)
	id, err := mgr.Create("bolt", match.CreateOptions{NumPlayers: 2})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	j0, _ := mgr.Join(id, "p0", match.JoinOptions{})
	mgr.Join(id, "p1", match.JoinOptions{})

	// P0 casts bolt — pauses for a target.
	if _, err := mgr.MoveReq(id, j0.PlayerID, j0.PlayerCredentials, core.MoveRequest{Move: "cast_bolt"}); err != nil {
		t.Fatalf("cast_bolt: %v", err)
	}
	m, _ := mgr.State(id)
	if len(m.State.Blocks) != 1 || m.State.Blocks[0].Tag != "creature" {
		t.Fatalf("expected one pending 'creature' block, got %#v", m.State.Blocks)
	}

	// A normal (non-resume) move must be rejected while blocked.
	if _, err := mgr.MoveReq(id, j0.PlayerID, j0.PlayerCredentials, core.MoveRequest{Move: "cast_bolt"}); !errors.Is(err, core.ErrBlocked) {
		t.Fatalf("expected ErrBlocked, got %v", err)
	}

	// Resume with the player's selection.
	if _, err := mgr.MoveReq(id, j0.PlayerID, j0.PlayerCredentials, core.MoveRequest{
		Move:      "resolve_bolt",
		ResumeTag: "creature",
		Args:      []any{"ent:1"},
	}); err != nil {
		t.Fatalf("resolve_bolt: %v", err)
	}

	m2, _ := mgr.State(id)
	if len(m2.State.Blocks) != 0 {
		t.Fatalf("block should be cleared after resume, got %#v", m2.State.Blocks)
	}
	sg := m2.State.G.(*StarlarkG)
	dead, _ := sg.Data["dead"].([]any)
	if len(dead) != 1 || dead[0] != "ent:1" {
		t.Fatalf("dead = %#v, want [ent:1]", sg.Data["dead"])
	}

	// Replay is byte-identical (block/resume reconstructs from the log).
	replayed, err := core.Replay(g, m2.State.Log, 2, nil)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	orig, _ := json.Marshal(m2.State.G)
	repl, _ := json.Marshal(replayed.G)
	if string(orig) != string(repl) {
		t.Fatalf("replay mismatch:\n orig=%s\n repl=%s", orig, repl)
	}
}
```

- [ ] **Step 2: Run it**

Run: `export PATH=$HOME/.local/go/bin:$PATH && cd mcp && go test ./starlarkgame/ -run TestBolt_RequestResolveReplay -v`
Outcomes:
- **Compile error**: cross-check `core.MoveRequest` fields and `core.ErrBlocked` / `core.Replay` signatures against the source; adjust. Re-run.
- **PASS**: commit.
- **Assertion/runtime failure**: a real issue — do NOT weaken the test. Diagnose precisely:
  - If `cast_bolt` produced no block: `mc.Queue` isn't threaded into `BridgeCtx` (Task 1 Step 4) or `ends_turn:False` isn't preventing an EndTurn that drains the block. Check the move closure.
  - If the resume errored with `ErrUnknownResumeTag`: the tag from `request_target` (`req.Kind`="creature") must equal the `ResumeTag` ("creature"). Confirm `RequestTarget` sets `Tag = req.Kind`.
  - If `ErrBlocked` was NOT returned for the second `cast_bolt`: the reducer's block gate may differ — read `core/reducer.go` around the ResumeTag/ErrBlocked handling and report.
  - If replay mismatches: the block/resume must reconstruct deterministically from the log; report the orig/repl strings and investigate whether `State.Blocks` or `Queue` is being serialized into `G`'s JSON (it shouldn't — they're separate `State` fields).

- [ ] **Step 3: Full suites**

Run: `export PATH=$HOME/.local/go/bin:$PATH && cd mcp && go test ./starlarkgame/ ./modulebridge/`
Expected: ok / ok.

- [ ] **Step 4: Commit**

```bash
cd mcp && git add starlarkgame/targetrequest_integration_test.go
git commit -m "starlarkgame: e2e TargetRequest block/resume + replay determinism test"
```

---

## Task 4: Documentation

**Files:** `mcp/docs/designed-games.md`.

- [ ] **Step 1: Read** the existing module docs section to match style/placement (the "land in later phases" trailer mentions target selection).

- [ ] **Step 2: Add a "Target selection (pausing for player input)" subsection** after the economy/shop subsection. Content:

```markdown
## Target selection (pausing for player input)

A move can pause to ask a player to choose targets, then resume on their pick.
In the requesting move call `ctx.request_target(kind=..., candidates=[...],
min=1, max=1)` and mark the move `"ends_turn": False`. The match pauses with a
pending request visible in `state.blocks`. Dispatch the follow-up via
`make_move` with `resumeTag` set to the request's `kind`; that resume move reads
`ctx.resuming_target()` and validates the player's pick with
`ctx.validate_selection([...])`.

    def cast_bolt(state, ctx):
        ctx.request_target(kind="creature", candidates=state["creatures"], min=1, max=1)
        return state

    def resolve_bolt(state, ctx, target):
        ctx.validate_selection([target])          # rejects an out-of-set pick
        # … apply the effect to `target` …
        return state

    MOVES = {
        "cast_bolt":    {"apply": cast_bolt, "ends_turn": False},
        "resolve_bolt": {"apply": resolve_bolt, "ends_turn": False,
                         "args": [{"name": "target", "type": "string"}]},
    }

`request_target` args: `kind` (required string; also the resume tag),
`candidates` (list — entity tokens, ints, or strings), `min`/`max` (selection
count; default 1/1), optional `source` and `data` (opaque context). While a
request is pending, ordinary moves are rejected until a `resumeTag` move clears
it. `ctx.resuming_target()` returns the pending request dict (or `None`);
`ctx.validate_selection(list)` rejects the move if the pick violates the
request's cardinality or candidate set.

Event hooks (HOOKS) land in a later phase.
```
(Adjust the trailing "later phases" line elsewhere in the doc so target selection is no longer listed as pending.)

- [ ] **Step 3: Build + full test + commit**

```bash
export PATH=$HOME/.local/go/bin:$PATH && cd mcp && go build ./... && go test ./starlarkgame/ ./modulebridge/
```
Expected: clean + green.
```bash
cd mcp && git add docs/designed-games.md && git commit -m "docs: document target selection (request_target/resume) for designed games"
```

---

## Self-Review

**Spec coverage:** Phase-4 scope (TargetRequest block/resume + `submit_target`-equivalent via `make_move resumeTag`) delivered: `request_target` (pause), `resuming_target` + `validate_selection` (resume), and the play-path `resumeTag`. ✓

**Placeholder scan:** No TBD/TODO; the Task 3 debug guidance is conditional diagnosis with concrete file references. ✓

**Type consistency:** `BridgeCtx.Queue *core.Queue`, `BridgeCtx.ResumingBlock *core.BlockSpec`; builtins `request_target`/`resuming_target`/`validate_selection`; `MakeMoveArgs.ResumeTag` → `core.MoveRequest.ResumeTag`. `request_target` kwargs (`kind`/`candidates`/`min`/`max`/`source`/`data`) match the `resuming_target` dict keys and `core.TargetRequest` fields. ✓

**Determinism:** No RNG introduced. Block/resume state lives in `State.Blocks`/`State.Queue` (engine-managed, replayable); the binding only reads/writes through the engine API. Replay proven in Task 3. ✓

**Engine-API risk:** The one assumption to verify early (Task 1 Step 2 / Task 3) is that `core.Queue` zero value is usable in a test (`&core.Queue{}` with exported `RequestTarget`/`PendingBlocks`) and that `MoveContext.Queue`/`ResumingBlock` are populated by the reducer as documented — both confirmed against `core/queue.go`, `core/move.go`, `core/reducer.go`.

**Known caveat (carry forward):** Multiple concurrent same-`kind` requests are not exposed (single-request common case only); use distinct kinds if a game needs concurrency. The remaining initiative item is phase 5 (HOOKS event bus + semantic arg types) and the still-unsolved seeded-RNG-to-ops threading.
```
