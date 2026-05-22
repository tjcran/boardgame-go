package mcp

import (
	"context"
	"testing"
)

// A spec that uses ctx.modules across setup (mutating), a move (mutating +
// read), and the read-side callbacks. playtest_draft must instantiate the
// declared modules so all of these run, exactly as a live match would.
const playtestModulesSpec = `
META = {"name": "pt-mods", "min_players": 1, "max_players": 1}
MODULES = ["tabletop"]
def setup(ctx):
    ctx.modules.tabletop.new_board(kind="square", w=3, h=3)
    ctx.modules.tabletop.place(unit=1, x=0, y=0)
    return {"placed": True}
def end_if(state, ctx):
    if len(ctx.modules.tabletop.entities_at(x=2, y=2)) > 0:
        return {"winner": "0"}
    return None
def legal_moves(state, ctx): return [{"name": "go", "args": []}]
def go(state, ctx):
    ctx.modules.tabletop.move(unit=1, x=2, y=2)
    p = ctx.modules.tabletop.position_of(unit=1)
    return {"px": p[0], "py": p[1]}
MOVES = {"go": {"apply": go}}
`

func TestPlaytestDraft_InstantiatesModules(t *testing.T) {
	res, err := (&Tools{}).PlaytestDraft(context.Background(), PlaytestDraftArgs{
		Source:   playtestModulesSpec,
		Scenario: []PlaytestStep{{PlayerID: "0", Move: "go"}},
	})
	if err != nil {
		t.Fatalf("PlaytestDraft err: %v", err)
	}
	if len(res.ValidationErrors) != 0 {
		t.Fatalf("validation errors: %v", res.ValidationErrors)
	}
	if res.SetupState["placed"] != true {
		t.Fatalf("setup did not run with modules; setup state = %v", res.SetupState)
	}
	if len(res.Trace) != 1 {
		t.Fatalf("want 1 trace step, got %d", len(res.Trace))
	}
	tr := res.Trace[0]
	if tr.Error != "" {
		t.Fatalf("move errored: %s", tr.Error)
	}
	if tr.StateAfter["px"] != int64(2) || tr.StateAfter["py"] != int64(2) {
		t.Fatalf("move did not read module position: after = %v", tr.StateAfter)
	}
	// end_if read the board (unit now at 2,2) and reported a winner.
	if tr.EndIfResult == nil {
		t.Fatalf("end_if did not read modules in the trace")
	}
}
