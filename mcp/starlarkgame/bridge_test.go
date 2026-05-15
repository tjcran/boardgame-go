package starlarkgame

import (
	"strings"
	"testing"
)

func TestBridgeCtxFields(t *testing.T) {
	src := `
def setup(ctx):
    return {
        "pid": ctx.player_id,
        "n":   ctx.num_players,
    }
`
	spec, err := compileSpecForTest(src)
	if err != nil { t.Fatalf("compile: %v", err) }
	bc := BridgeCtx{PlayerID: "1", NumPlayers: 3}
	got, err := spec.callForTest("setup", []any{bc})
	if err != nil { t.Fatalf("call: %v", err) }
	m := got.(map[string]any)
	if m["pid"] != "1" || m["n"] != int64(3) {
		t.Fatalf("ctx fields wrong: %#v", m)
	}
}

func TestBridgeCtxLogAppends(t *testing.T) {
	src := `
def setup(ctx):
    ctx.log("hello")
    ctx.log("world")
    return {}
`
	spec, _ := compileSpecForTest(src)
	bc := BridgeCtx{}
	_, _ = spec.callForTest("setup", []any{&bc})
	if len(bc.Log) != 2 || bc.Log[0] != "hello" {
		t.Fatalf("log: %#v", bc.Log)
	}
	if !strings.Contains(bc.Log[1], "world") {
		t.Fatalf("log[1]: %q", bc.Log[1])
	}
}
