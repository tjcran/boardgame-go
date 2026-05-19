# Core: typed TargetRequest protocol

## Context

The engine already has a working "pause for player input" mechanic via `core.Queue.Block(tag, playerID, data)`, with the reducer enforcing `ErrBlocked` until a move arrives carrying a matching `ResumeTag`. State.Blocks is JSON-serialized and visible to clients today.

The gap: `BlockSpec.Data` is `any`. There's no typed shape that tells a server / client / replay layer "this block is a target request, here are the candidate options, the player must choose between `Min` and `Max` of them." Every game that needs mid-move targeting reinvents this — declaring its own payload shape, validating selections by hand, and giving clients no canonical way to render a target picker.

Closing this gap is engine-level (it shapes the transport contract), small in surface area (one struct, one optional field, one helper, one MoveContext accessor), and broadly useful:

- Chess: pawn-promotion choice.
- Mancala: which pile to scoop.
- Drafting games: pick a card from the offered set.
- Any CCG: pick the target for a `damage` / `discard` / `counter` effect.
- Tabletop: pick a unit / hex from a candidate set.

The library ships the **shape and helpers**; games own which `Kind` strings mean what, how `Candidates` map to their domain types, and what the resume move does with the player's pick. Same pattern as `ccg.Effect`.

## Design

### `core.TargetRequest` (new, in `core/target.go`)

```go
type TargetRequest struct {
    Kind       string         `json:"kind"`              // game-defined verb: "damage", "promote", "discard"
    Candidates []any          `json:"candidates,omitempty"`
    Min        int            `json:"min,omitempty"`     // minimum picks (default 1 when unset, treated as required)
    Max        int            `json:"max,omitempty"`     // maximum picks (0 = same as Min)
    Source     any            `json:"source,omitempty"`  // optional: who/what asked (card ID, hex, etc.)
    Data       map[string]any `json:"data,omitempty"`    // optional: arbitrary extras (mode flags, etc.)
}
```

`Candidates` are `[]any` so the same shape covers `ccg.EntityID`, `tabletop.Pos`, plain strings, ints — whatever the game addresses. JSON round-trips correctly because the JSON encoder is uniform on slices of any.

### Extend `BlockSpec` with an optional typed `Target`

```go
type BlockSpec struct {
    Tag      string          `json:"tag"`
    PlayerID string          `json:"playerID"`
    Order    int             `json:"order,omitempty"`
    Data     any             `json:"data,omitempty"`     // existing free-form payload
    Target   *TargetRequest  `json:"target,omitempty"`   // typed shape for target-request blocks
}
```

Adding a field, not replacing one. Pure pauses (e.g. "wait for confirmation") leave `Target` nil. Existing games that call `Queue.Block(...)` directly keep working unchanged.

### Queue helper

```go
func (q *Queue) RequestTarget(playerID string, req TargetRequest) {
    q.blocks = append(q.blocks, BlockSpec{
        Tag:      req.Kind,
        PlayerID: playerID,
        Target:   &req,
    })
}
```

Canonical `Tag = req.Kind` — keeps the resume contract simple. Games that need multiple concurrent requests of the same Kind can either tag-disambiguate by hand (call `Queue.Block` directly) or include a discriminator in `req.Data`.

### `MoveContext.ResumingBlock`

The reducer currently consumes a matched block from `State.Blocks` *before* the resume move runs, so the resume move has no way to see what it's resuming. Add:

```go
type MoveContext struct {
    // …existing fields…
    ResumingBlock *BlockSpec  // the block this move resumed, if any; nil for non-resume moves
}
```

In `ApplyContext`, capture the consumed block and stash it on the MoveContext before invoking the move. The resume move can then do:

```go
req := mc.ResumingBlock.Target
if err := core.ValidateSelection(*req, args); err != nil {
    return mc.G, err
}
```

Drain steps (`applyStep`) don't process resume tags, so `ResumingBlock` stays nil there. That's correct — drain-step moves are server-driven, not player-input responses.

### `ValidateSelection` helper

```go
var ErrTargetCardinality = errors.New("selection count out of range")
var ErrTargetNotCandidate = errors.New("selection not in candidate set")

func ValidateSelection(req TargetRequest, sel []any) error {
    min, max := req.Min, req.Max
    if min == 0 { min = 1 }
    if max == 0 { max = min }
    if len(sel) < min || len(sel) > max {
        return fmt.Errorf("%w: got %d, want %d..%d", ErrTargetCardinality, len(sel), min, max)
    }
    if len(req.Candidates) == 0 {
        return nil  // open-ended request (free text, arbitrary coords) — game validates Data itself
    }
    for _, s := range sel {
        if !containsAny(req.Candidates, s) {
            return fmt.Errorf("%w: %v", ErrTargetNotCandidate, s)
        }
    }
    return nil
}
```

`containsAny` uses `reflect.DeepEqual` to compare — works for primitives, `ccg.EntityID`, `tabletop.Pos`, and any value-typed game ID. Note: JSON-decoded numbers come back as `float64`, so games with `int`-typed candidates that travel through the wire need to compare consistently — `ValidateSelection` handles this by normalizing both sides through a numeric fold (int / int64 / float64 → float64) before `DeepEqual` on non-numeric kinds. Documented behavior, not a foot-gun.

### What this plan deliberately does NOT include

- **No client-side UI / picker** — that's the transport's concern. The server publishes `State.Blocks` with typed `Target`, and the client renders accordingly.
- **No automatic Args-as-selection helper** — the resume move receives `args ...any` as before. `ValidateSelection(*req, args)` is one call, and games may legitimately want extra args beyond the selection.
- **No multi-step targeting state machine** — games chain `RequestTarget` calls themselves by pushing another block from the resume move.
- **No replacement for `Queue.Block`** — direct `Block` calls remain the right tool for non-target pauses (confirmation, "wait for opponent's response," etc.).
- **No tabletop / ccg integration changes** — those modules keep working unchanged. Future PRs can wire their domain types through `TargetRequest.Candidates` as game-author ergonomics.

## Files

**New:**
- `core/target.go` — `TargetRequest` struct, `ValidateSelection`, sentinel errors.
- `core/target_test.go` — end-to-end: move pushes RequestTarget, drain pauses, resume move reads `mc.ResumingBlock.Target`, validates via `ValidateSelection`, applies the selection; bad cardinality rejected; non-candidate rejected; numeric coercion across JSON round-trip; pure `Queue.Block` calls still work (no Target).

**Modified:**
- `core/queue.go` — add `BlockSpec.Target *TargetRequest`; add `Queue.RequestTarget(playerID, req)` helper.
- `core/move.go` — add `MoveContext.ResumingBlock *BlockSpec`.
- `core/reducer.go` — capture the consumed block in `ApplyContext`; assign to `mc.ResumingBlock` before invoking the move.

**Reused (no changes):**
- Existing `ErrBlocked` / `ErrUnknownResumeTag` flow, `State.Blocks` serialization, `MoveRequest.ResumeTag` matching.

## Verification

1. `go test ./core/...` — all existing block / resume / drain tests pass; new typed-target tests pass.
2. `go test ./...` — full suite (server, storage, games, modules) green.
3. `go vet ./...` clean.
4. JSON round-trip test: marshal/unmarshal a State with one TargetRequest-bearing block; confirm Kind, Candidates, Min/Max, Source survive.
5. Backward-compat check: rerun `TestQueueBlockPausesCascade` and friends (they call `Queue.Block` directly with nil Data) — must still pass without modification.
6. Resume-move ergonomics: confirm `mc.ResumingBlock` is non-nil only inside a move with a matched `ResumeTag`, and nil for both pure external moves and drain steps.
