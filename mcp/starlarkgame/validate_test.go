package starlarkgame

import (
	"context"
	"strings"
	"testing"
)

func TestValidateAcceptsGoodSpec(t *testing.T) {
	spec, _ := LoadSpec(goodSpec)
	if err := Validate(context.Background(), spec); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestValidateRejectsGameEndedAtSetup(t *testing.T) {
	spec, _ := LoadSpec(`
META = {"name":"x","min_players":2,"max_players":2}
def setup(ctx): return {}
MOVES = {"noop": {"args":[], "apply": lambda s, c: s}}
def end_if(state, ctx): return {"draw": True}
def legal_moves(state, ctx): return []
`)
	err := Validate(context.Background(), spec)
	if err == nil || !strings.Contains(err.Error(), "end_if") {
		t.Fatalf("expected end_if-at-setup error, got %v", err)
	}
}

func TestValidateRejectsBadLegalMoveName(t *testing.T) {
	spec, _ := LoadSpec(`
META = {"name":"x","min_players":2,"max_players":2}
def setup(ctx): return {}
MOVES = {"real": {"args":[], "apply": lambda s, c: s}}
def end_if(state, ctx): return None
def legal_moves(state, ctx): return [{"name":"phantom","args":[]}]
`)
	err := Validate(context.Background(), spec)
	if err == nil || !strings.Contains(err.Error(), "phantom") {
		t.Fatalf("expected unknown-move error, got %v", err)
	}
}

func TestValidate_RejectsUndeclaredModuleUse(t *testing.T) {
	src := `
META = {"name": "m", "min_players": 1, "max_players": 1}
def setup(ctx):
    ctx.modules.ccg.new_zone(name="hand", ordered=False)
    return {}
def end_if(state, ctx): return None
def legal_moves(state, ctx): return []
MOVES = {"noop": {"apply": lambda state, ctx: state}}
`
	spec, err := LoadSpec(src)
	if err != nil {
		t.Fatalf("LoadSpec: %v", err)
	}
	if err := Validate(context.Background(), spec); err == nil {
		t.Fatal("expected Validate to reject ctx.modules.ccg use without MODULES=[\"ccg\"]")
	}
}
