package starlarkgame

import (
	"context"
	"testing"

	"github.com/tjcran/boardgame-go/core"
)

func TestLegalMoveName_AcceptsBothKeys(t *testing.T) {
	if got := legalMoveName(map[string]any{"name": "advance"}); got != "advance" {
		t.Errorf("name-only: got %q", got)
	}
	if got := legalMoveName(map[string]any{"move": "advance"}); got != "advance" {
		t.Errorf("move-only: got %q", got)
	}
	// move wins when both present — matches the documented preferred key.
	if got := legalMoveName(map[string]any{"name": "old", "move": "new"}); got != "new" {
		t.Errorf("both: got %q, want new", got)
	}
	if got := legalMoveName(map[string]any{}); got != "" {
		t.Errorf("empty: got %q", got)
	}
}

// TestEnumerateWithMoveKey verifies that a spec written with "move":... in
// its legal_moves return value is enumerated correctly through the
// BuildCoreGame adapter — the in-the-wild bug that motivated the fix.
func TestEnumerateWithMoveKey(t *testing.T) {
	src := `
META = {"name":"k","min_players":2,"max_players":2}
def setup(ctx): return {"x": 0}
def _go(state, ctx): pass
MOVES = {"go": {"args":[], "apply": _go}}
def end_if(state, ctx): return None
def legal_moves(state, ctx):
    return [{"move":"go","args":[]}]
`
	spec, err := LoadSpec(src)
	if err != nil {
		t.Fatalf("LoadSpec: %v", err)
	}
	if err := Validate(context.Background(), spec); err != nil {
		t.Fatalf("Validate must accept move-keyed legal_moves: %v", err)
	}
	g := BuildCoreGame(spec)
	state := g.Setup(core.Ctx{NumPlayers: 2}, nil)
	acts := g.Enumerate(state, core.Ctx{NumPlayers: 2, CurrentPlayer: "0"}, "0")
	if len(acts) != 1 || acts[0].Move != "go" {
		t.Fatalf("expected [{Move:go}], got %#v", acts)
	}
}
