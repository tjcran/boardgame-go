package starlarkgame

import (
	"context"
	"reflect"
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
