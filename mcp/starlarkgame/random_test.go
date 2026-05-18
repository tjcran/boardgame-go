package starlarkgame

import (
	"context"
	"testing"
)

func TestCtxRandomIsDeterministic(t *testing.T) {
	spec, err := LoadSpec(`
META = {"name":"r","min_players":1,"max_players":1}
def setup(ctx):
    return {"draws": [ctx.random.range(100) for _ in range(5)]}
MOVES = {"noop": {"args":[], "apply": lambda s, c: s}}
def end_if(state, ctx): return None
def legal_moves(state, ctx): return []
`)
	if err != nil {
		t.Fatalf("LoadSpec: %v", err)
	}

	bc1 := &BridgeCtx{}
	bc1.AttachSeededRandom(1234)
	state1, err := spec.CallSetup(context.Background(), bc1)
	if err != nil {
		t.Fatalf("CallSetup 1: %v", err)
	}

	bc2 := &BridgeCtx{}
	bc2.AttachSeededRandom(1234)
	state2, err := spec.CallSetup(context.Background(), bc2)
	if err != nil {
		t.Fatalf("CallSetup 2: %v", err)
	}

	if !equalAny(state1["draws"], state2["draws"]) {
		t.Fatalf("same seed produced different sequences: %v vs %v", state1["draws"], state2["draws"])
	}

	bc3 := &BridgeCtx{}
	bc3.AttachSeededRandom(9999)
	state3, err := spec.CallSetup(context.Background(), bc3)
	if err != nil {
		t.Fatalf("CallSetup 3: %v", err)
	}
	if equalAny(state1["draws"], state3["draws"]) {
		t.Fatalf("different seeds produced same sequence")
	}
}

func equalAny(a, b any) bool {
	la, lb := a.([]any), b.([]any)
	if len(la) != len(lb) {
		return false
	}
	for i := range la {
		if la[i] != lb[i] {
			return false
		}
	}
	return true
}
