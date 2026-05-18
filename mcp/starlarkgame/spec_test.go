package starlarkgame

import (
	"strings"
	"testing"
)

const goodSpec = `
META = {
    "name": "demo",
    "min_players": 2,
    "max_players": 2,
    "description": "A demo.",
}
def setup(ctx): return {}
MOVES = {"noop": {"args": [], "apply": lambda state, ctx: state}}
def end_if(state, ctx): return None
def legal_moves(state, ctx): return []
`

func TestLoadSpecHappyPath(t *testing.T) {
	s, err := LoadSpec(goodSpec)
	if err != nil { t.Fatalf("LoadSpec: %v", err) }
	if s.Meta.Name != "demo" {
		t.Errorf("Name = %q, want demo", s.Meta.Name)
	}
	if s.Meta.MinPlayers != 2 || s.Meta.MaxPlayers != 2 {
		t.Errorf("players: %d-%d", s.Meta.MinPlayers, s.Meta.MaxPlayers)
	}
	if _, ok := s.Moves["noop"]; !ok {
		t.Errorf("noop move not registered")
	}
}

func TestLoadSpecMissingMeta(t *testing.T) {
	src := `def setup(ctx): return {}`
	_, err := LoadSpec(src)
	if err == nil || !strings.Contains(err.Error(), "META") {
		t.Fatalf("expected META-missing error, got %v", err)
	}
}

func TestLoadSpecBadName(t *testing.T) {
	src := strings.Replace(goodSpec, `"demo"`, `"BAD NAME"`, 1)
	_, err := LoadSpec(src)
	if err == nil || !strings.Contains(err.Error(), "name") {
		t.Fatalf("expected name-format error, got %v", err)
	}
}
