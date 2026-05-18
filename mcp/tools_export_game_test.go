package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/tjcran/boardgame-go/games/tictactoe"
	"github.com/tjcran/boardgame-go/match"
	"github.com/tjcran/boardgame-go/storage"
)

func TestExportGame_HappyPath(t *testing.T) {
	mgr := match.NewManager(storage.NewMemory())
	mgr.MustRegister(tictactoe.New())
	reg := NewUserAwareRegistry(mgr, NewInMemoryUserGames())
	tools := &Tools{Manager: mgr, Registry: reg}
	ctx := contextWithUser(context.Background(), "alice")

	const src = `
META = {"name":"hex-lite","min_players":2,"max_players":2,"description":"A toy."}
def setup(ctx): return {"x": 0}
def _click(state, ctx, idx):
    return {"x": state["x"] + idx}
def _pass(state, ctx): return state
MOVES = {
    "click": {"args":[{"name":"idx","type":"int","min":0,"max":3}], "apply": _click},
    "pass":  {"args":[], "apply": _pass},
}
def end_if(state, ctx): return None
def legal_moves(state, ctx): return [{"move":"pass","args":[]}]
`
	if _, err := tools.RegisterGame(ctx, RegisterGameArgs{Source: src, LLMGuide: "Pass patiently."}); err != nil {
		t.Fatalf("RegisterGame: %v", err)
	}

	out, err := tools.ExportGame(ctx, ExportGameArgs{Name: "hex-lite"})
	if err != nil {
		t.Fatalf("ExportGame: %v", err)
	}
	if out.Name != "hex-lite" {
		t.Errorf("Name = %q", out.Name)
	}
	if out.SpecStar != src {
		t.Errorf("SpecStar round-trip lost content")
	}

	// SkillMD has the expected sections.
	for _, want := range []string{
		"---\nname: hex-lite",
		"description: A toy.",
		"owner: alice",
		"## Moves",
		"`click(idx: int [0..3])`",
		"`pass()`",
		"## Designer's notes",
		"Pass patiently.",
		"## Strategy",
	} {
		if !strings.Contains(out.SkillMD, want) {
			t.Errorf("SkillMD missing %q\n---\n%s", want, out.SkillMD)
		}
	}

	// Manifest has structured metadata.
	if out.Manifest.Name != "hex-lite" || out.Manifest.MinPlayers != 2 || out.Manifest.MaxPlayers != 2 {
		t.Errorf("manifest meta: %+v", out.Manifest)
	}
	if out.Manifest.Owner != "alice" {
		t.Errorf("manifest owner = %q", out.Manifest.Owner)
	}
	if len(out.Manifest.Moves) != 2 {
		t.Fatalf("manifest moves: %+v", out.Manifest.Moves)
	}
	// Moves sorted alphabetically.
	if out.Manifest.Moves[0].Name != "click" || out.Manifest.Moves[1].Name != "pass" {
		t.Errorf("moves not sorted: %+v", out.Manifest.Moves)
	}
	if len(out.Manifest.Moves[0].Args) != 1 || out.Manifest.Moves[0].Args[0].Name != "idx" {
		t.Errorf("click args missing: %+v", out.Manifest.Moves[0].Args)
	}
}

func TestExportGame_RejectsBuiltin(t *testing.T) {
	mgr := match.NewManager(storage.NewMemory())
	mgr.MustRegister(tictactoe.New())
	reg := NewUserAwareRegistry(mgr, NewInMemoryUserGames())
	tools := &Tools{Manager: mgr, Registry: reg}
	ctx := contextWithUser(context.Background(), "alice")

	_, err := tools.ExportGame(ctx, ExportGameArgs{Name: "tic-tac-toe"})
	if err == nil {
		t.Fatalf("expected error exporting built-in")
	}
	if !strings.Contains(err.Error(), "built-in") {
		t.Errorf("error should mention built-in; got %v", err)
	}
}

func TestExportGame_RefusesCrossOwner(t *testing.T) {
	mgr := match.NewManager(storage.NewMemory())
	reg := NewUserAwareRegistry(mgr, NewInMemoryUserGames())
	tools := &Tools{Manager: mgr, Registry: reg}

	const src = `
META = {"name":"private","min_players":2,"max_players":2}
def setup(ctx): return {}
def _noop(state, ctx): return state
MOVES = {"noop":{"args":[],"apply":_noop}}
def end_if(state, ctx): return None
def legal_moves(state, ctx): return [{"move":"noop","args":[]}]
`
	aliceCtx := contextWithUser(context.Background(), "alice")
	if _, err := tools.RegisterGame(aliceCtx, RegisterGameArgs{Source: src}); err != nil {
		t.Fatalf("RegisterGame: %v", err)
	}

	bobCtx := contextWithUser(context.Background(), "bob")
	_, err := tools.ExportGame(bobCtx, ExportGameArgs{Name: "private"})
	if err == nil {
		t.Fatalf("expected refusal exporting another user's game")
	}
}

func TestExportGame_RejectsRegistryNotConfigured(t *testing.T) {
	tools := &Tools{}
	_, err := tools.ExportGame(context.Background(), ExportGameArgs{Name: "x"})
	if err == nil || !strings.Contains(err.Error(), "registry") {
		t.Fatalf("expected 'registry not configured', got %v", err)
	}
}
