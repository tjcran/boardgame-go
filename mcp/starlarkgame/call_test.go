package starlarkgame

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestCallSetupReturnsInitialState(t *testing.T) {
	spec, err := LoadSpec(`
META = {"name":"demo","min_players":2,"max_players":2}
def setup(ctx):
    return {"cells": [None]*3, "pid_at_setup": ctx.player_id}
MOVES = {"noop": {"args":[], "apply": lambda state, ctx: None}}
def end_if(state, ctx): return None
def legal_moves(state, ctx): return []
`)
	if err != nil {
		t.Fatalf("LoadSpec: %v", err)
	}
	bc := &BridgeCtx{PlayerID: "", NumPlayers: 2}
	got, err := spec.CallSetup(context.Background(), bc)
	if err != nil {
		t.Fatalf("CallSetup: %v", err)
	}
	want := map[string]any{
		"cells":        []any{nil, nil, nil},
		"pid_at_setup": "",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("CallSetup = %#v, want %#v", got, want)
	}
}

func TestCallMoveMutatesState(t *testing.T) {
	spec, err := LoadSpec(`
META = {"name":"d","min_players":2,"max_players":2}
def setup(ctx): return {"cells": [None, None, None]}
def _click(state, ctx, idx):
    if state["cells"][idx] != None:
        fail("occupied")
    state["cells"][idx] = ctx.player_id
MOVES = {"click": {"args":[{"name":"idx","type":"int"}], "apply": _click}}
def end_if(state, ctx): return None
def legal_moves(state, ctx): return []
`)
	if err != nil { t.Fatalf("LoadSpec: %v", err) }
	bc := &BridgeCtx{PlayerID: "0", NumPlayers: 2}
	state, err := spec.CallSetup(context.Background(), bc)
	if err != nil { t.Fatalf("CallSetup: %v", err) }

	newState, err := spec.CallMove(context.Background(), bc, "click", state, []any{int64(1)})
	if err != nil { t.Fatalf("CallMove: %v", err) }
	cells := newState["cells"].([]any)
	if cells[1] != "0" { t.Fatalf("cells[1] = %v, want '0'", cells[1]) }
}

func TestCallMoveRejectsIllegal(t *testing.T) {
	spec, err := LoadSpec(`
META = {"name":"d","min_players":2,"max_players":2}
def setup(ctx): return {"x": 0}
def _do(state, ctx):
    fail("nope")
MOVES = {"go": {"args":[], "apply": _do}}
def end_if(state, ctx): return None
def legal_moves(state, ctx): return []
`)
	if err != nil { t.Fatalf("LoadSpec: %v", err) }
	bc := &BridgeCtx{PlayerID: "0", NumPlayers: 2}
	state, _ := spec.CallSetup(context.Background(), bc)
	_, err = spec.CallMove(context.Background(), bc, "go", state, nil)
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("expected 'nope' error, got %v", err)
	}
}
