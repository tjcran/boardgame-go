package mcp

import (
	"context"
	"strings"
	"testing"
)

func TestPlaytest_GoodSpecReturnsTrace(t *testing.T) {
	tools := &Tools{} // playtest doesn't need registry/manager
	const src = `
META = {"name":"d","min_players":2,"max_players":2}
def setup(ctx): return {"cells": [None]*3}
def _click(state, ctx, idx):
    if state["cells"][idx] != None: fail("occupied")
    state["cells"][idx] = ctx.player_id
MOVES = {"click": {"args":[{"name":"idx","type":"int"}], "apply": _click}}
def end_if(state, ctx): return None
def legal_moves(state, ctx):
    return [{"name":"click","args":[i]} for i in range(3) if state["cells"][i] == None]
`
	out, err := tools.PlaytestDraft(context.Background(), PlaytestDraftArgs{
		Source: src,
		Scenario: []PlaytestStep{
			{PlayerID: "0", Move: "click", Args: []any{int64(0)}},
			{PlayerID: "1", Move: "click", Args: []any{int64(1)}},
		},
	})
	if err != nil {
		t.Fatalf("PlaytestDraft: %v", err)
	}
	if len(out.Trace) != 2 {
		t.Fatalf("trace len = %d", len(out.Trace))
	}
	if out.SetupState == nil {
		t.Fatalf("SetupState nil")
	}
}

func TestPlaytest_BadSpecReturnsErrors(t *testing.T) {
	tools := &Tools{}
	out, err := tools.PlaytestDraft(context.Background(), PlaytestDraftArgs{Source: `not valid starlark`})
	if err != nil {
		t.Fatalf("PlaytestDraft itself errored: %v", err)
	}
	if len(out.ValidationErrors) == 0 || !strings.Contains(out.ValidationErrors[0], "parse") {
		t.Fatalf("expected parse error, got %v", out.ValidationErrors)
	}
}
