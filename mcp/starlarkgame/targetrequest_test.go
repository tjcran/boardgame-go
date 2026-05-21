package starlarkgame

import (
	"context"
	"testing"

	"github.com/tjcran/boardgame-go/core"
)

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

	got, err := spec.CallMove(context.Background(), bc, "resolve", map[string]any{}, []any{"ent:1"})
	if err != nil {
		t.Fatalf("valid resolve: %v", err)
	}
	if got["kind"] != "creature" || got["picked"] != "ent:1" {
		t.Fatalf("resolve result wrong: %#v", got)
	}

	if _, err := spec.CallMove(context.Background(), bc, "resolve", map[string]any{}, []any{"ent:9"}); err == nil {
		t.Fatal("expected validate_selection to reject ent:9")
	}
}
