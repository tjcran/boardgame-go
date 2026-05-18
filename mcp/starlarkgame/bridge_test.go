package starlarkgame

import (
	"context"
	"testing"
)

func TestBridgeCtxFields(t *testing.T) {
	spec, err := LoadSpec(`
META = {"name":"t","min_players":1,"max_players":3}
def setup(ctx): return {"pid": ctx.player_id, "n": ctx.num_players}
MOVES = {"noop": {"args":[], "apply": lambda s, c: s}}
def end_if(state, ctx): return None
def legal_moves(state, ctx): return []
`)
	if err != nil {
		t.Fatalf("LoadSpec: %v", err)
	}
	bc := &BridgeCtx{PlayerID: "1", NumPlayers: 3}
	got, err := spec.CallSetup(context.Background(), bc)
	if err != nil {
		t.Fatalf("CallSetup: %v", err)
	}
	if got["pid"] != "1" || got["n"] != int64(3) {
		t.Fatalf("ctx fields wrong: %#v", got)
	}
}

func TestBridgeCtxLogAppends(t *testing.T) {
	spec, err := LoadSpec(`
META = {"name":"t","min_players":1,"max_players":1}
def setup(ctx):
    ctx.log("hello")
    ctx.log("world")
    return {}
MOVES = {"noop": {"args":[], "apply": lambda s, c: s}}
def end_if(state, ctx): return None
def legal_moves(state, ctx): return []
`)
	if err != nil {
		t.Fatalf("LoadSpec: %v", err)
	}
	bc := &BridgeCtx{}
	if _, err := spec.CallSetup(context.Background(), bc); err != nil {
		t.Fatalf("CallSetup: %v", err)
	}
	if len(bc.Log) != 2 || bc.Log[0] != "hello" || bc.Log[1] != "world" {
		t.Fatalf("log: %#v", bc.Log)
	}
}
