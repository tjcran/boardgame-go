package starlarkgame

import (
	"context"
	"strings"
	"testing"
)

// TestApplyMustReturnDict — the new contract: apply's return value is the
// new state. Returning None is rejected with a clear error pointing at
// the right fix (return a dict; or call fail()).
func TestApplyMustReturnDict_RejectsNoneReturn(t *testing.T) {
	spec, err := LoadSpec(`
META = {"name":"n","min_players":2,"max_players":2}
def setup(ctx): return {"v": 0}
def _bad(state, ctx): return None
MOVES = {"go": {"args":[], "apply": _bad}}
def end_if(state, ctx): return None
def legal_moves(state, ctx): return [{"move":"go","args":[]}]
`)
	if err != nil {
		t.Fatalf("LoadSpec: %v", err)
	}
	bc := &BridgeCtx{NumPlayers: 2, PlayerID: "0"}
	bc.AttachSeededRandom(0)
	state, _ := spec.CallSetup(context.Background(), bc)
	_, err = spec.CallMove(context.Background(), bc, "go", state, nil)
	if err == nil {
		t.Fatalf("expected error from apply returning None")
	}
	if !strings.Contains(err.Error(), "returned None") {
		t.Errorf("error should mention None-return contract; got %v", err)
	}
}

// TestApplyMustReturnDict_RejectsNonDictReturn — returning a string (the
// stale "return 'invalid'" pattern from pre-v0.5 specs) errors clearly
// instead of silently nooping.
func TestApplyMustReturnDict_RejectsNonDictReturn(t *testing.T) {
	spec, _ := LoadSpec(`
META = {"name":"n2","min_players":2,"max_players":2}
def setup(ctx): return {"v": 0}
def _bad(state, ctx): return "invalid"
MOVES = {"go": {"args":[], "apply": _bad}}
def end_if(state, ctx): return None
def legal_moves(state, ctx): return [{"move":"go","args":[]}]
`)
	bc := &BridgeCtx{NumPlayers: 2, PlayerID: "0"}
	bc.AttachSeededRandom(0)
	state, _ := spec.CallSetup(context.Background(), bc)
	_, err := spec.CallMove(context.Background(), bc, "go", state, nil)
	if err == nil {
		t.Fatalf("expected error from apply returning a string")
	}
	if !strings.Contains(err.Error(), "must return a state dict") {
		t.Errorf("error should mention state-dict contract; got %v", err)
	}
}

// TestApply_FrozenStateRejectsMutation — pre-v0.5 specs that tried to
// mutate inputs (the previous canonical style) now raise loudly so the
// author can switch styles.
func TestApply_FrozenStateRejectsMutation(t *testing.T) {
	spec, _ := LoadSpec(`
META = {"name":"n3","min_players":2,"max_players":2}
def setup(ctx): return {"cells": [0, 0, 0]}
def _bad(state, ctx, idx):
    state["cells"][idx] = 1
    return state
MOVES = {"poke": {"args":[{"name":"idx","type":"int"}], "apply": _bad}}
def end_if(state, ctx): return None
def legal_moves(state, ctx): return [{"move":"poke","args":[0]}]
`)
	bc := &BridgeCtx{NumPlayers: 2, PlayerID: "0"}
	bc.AttachSeededRandom(0)
	state, _ := spec.CallSetup(context.Background(), bc)
	_, err := spec.CallMove(context.Background(), bc, "poke", state, []any{int64(0)})
	if err == nil {
		t.Fatalf("expected mutation-of-frozen-state error")
	}
	// The Starlark runtime message describes the frozen mutation.
	if !strings.Contains(strings.ToLower(err.Error()), "frozen") {
		t.Errorf("error should mention frozen state; got %v", err)
	}
}
