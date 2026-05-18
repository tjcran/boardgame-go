package starlarkgame

import (
	"strings"
	"testing"
	"time"
)

const exportTestSpec = `
META = {
    "name": "demo-export",
    "min_players": 2,
    "max_players": 2,
    "description": "A tiny game for export-skeleton tests.",
}
def setup(ctx): return {"x": 0}
def _click(state, ctx, idx):
    new_x = state["x"] + idx
    return {"x": new_x}
def _pass(state, ctx): return state
MOVES = {
    "click": {"args":[{"name":"idx","type":"int","min":0,"max":8}], "apply": _click},
    "pass":  {"args":[], "apply": _pass},
}
def end_if(state, ctx): return None
def legal_moves(state, ctx): return [{"move":"pass","args":[]}]
`

func TestBuildSkillSkeleton_PopulatesMetadataAndMoves(t *testing.T) {
	spec, err := LoadSpec(exportTestSpec)
	if err != nil {
		t.Fatalf("LoadSpec: %v", err)
	}
	stamp := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	sk := BuildSkillSkeleton(spec, "Play patient.", "alice", stamp)

	if sk.Name != "demo-export" || sk.Description != "A tiny game for export-skeleton tests." {
		t.Errorf("metadata: %+v", sk)
	}
	if sk.MinPlayers != 2 || sk.MaxPlayers != 2 {
		t.Errorf("players: %d-%d", sk.MinPlayers, sk.MaxPlayers)
	}
	if sk.Owner != "alice" || sk.LLMGuide != "Play patient." || !sk.CreatedAt.Equal(stamp) {
		t.Errorf("misc: %+v", sk)
	}

	if len(sk.Moves) != 2 {
		t.Fatalf("expected 2 moves, got %d", len(sk.Moves))
	}
	if sk.Moves[0].Name != "click" || sk.Moves[1].Name != "pass" {
		t.Errorf("moves not sorted alphabetically: %+v", sk.Moves)
	}
	if len(sk.Moves[0].Args) != 1 || sk.Moves[0].Args[0].Name != "idx" {
		t.Errorf("click args: %+v", sk.Moves[0].Args)
	}
}

func TestSkillSkeleton_RenderMarkdown_HasExpectedAnchors(t *testing.T) {
	spec, _ := LoadSpec(exportTestSpec)
	sk := BuildSkillSkeleton(spec, "Strategy hint here.", "alice", time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC))
	md := sk.RenderMarkdown()

	for _, want := range []string{
		"---\nname: demo-export\n",
		"description: A tiny game for export-skeleton tests.",
		"owner: alice",
		"created_at: 2026-05-18T12:00:00Z",
		"# demo-export",
		"**Players:** 2–2.",
		"## Moves",
		"`click(idx: int [0..8])`",
		"`pass()`",
		"## Designer's notes",
		"Strategy hint here.",
		"## Strategy",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("rendered markdown missing %q\nfull output:\n%s", want, md)
		}
	}
}

func TestSkillSkeleton_RenderMarkdown_OmitsEmptyOptionals(t *testing.T) {
	spec, _ := LoadSpec(`
META = {"name":"bare","min_players":2,"max_players":2}
def setup(ctx): return {}
def _noop(state, ctx): return state
MOVES = {"noop": {"args":[], "apply": _noop}}
def end_if(state, ctx): return None
def legal_moves(state, ctx): return [{"move":"noop","args":[]}]
`)
	sk := BuildSkillSkeleton(spec, "", "", time.Time{})
	md := sk.RenderMarkdown()

	for _, unwanted := range []string{"description:", "owner:", "created_at:", "Designer's notes"} {
		if strings.Contains(md, unwanted) {
			t.Errorf("rendered markdown unexpectedly contains %q\n%s", unwanted, md)
		}
	}
}
