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

func TestLoadSpec_ParsesModules(t *testing.T) {
	src := `
META = {"name": "m", "min_players": 2, "max_players": 2}
MODULES = ["ccg"]
def setup(ctx): return {}
def end_if(state, ctx): return None
def legal_moves(state, ctx): return []
MOVES = {"pass": {"apply": lambda state, ctx: state}}
`
	s, err := LoadSpec(src)
	if err != nil {
		t.Fatalf("LoadSpec: %v", err)
	}
	if len(s.Modules) != 1 || s.Modules[0] != "ccg" {
		t.Fatalf("got modules %v, want [ccg]", s.Modules)
	}
}

func TestLoadSpec_RejectsUnknownModule(t *testing.T) {
	src := `
META = {"name": "m", "min_players": 2, "max_players": 2}
MODULES = ["nope"]
def setup(ctx): return {}
def end_if(state, ctx): return None
def legal_moves(state, ctx): return []
MOVES = {"pass": {"apply": lambda state, ctx: state}}
`
	if _, err := LoadSpec(src); err == nil {
		t.Fatal("expected error for unknown module")
	}
}

func TestLoadSpec_ParsesHooks(t *testing.T) {
	s, err := LoadSpec(`
META = {"name":"h","min_players":1,"max_players":1}
MODULES = ["ccg"]
def setup(ctx): return {}
def end_if(state, ctx): return None
def legal_moves(state, ctx): return []
def on_died(event, ctx): pass
def on_drawn(event, ctx): pass
HOOKS = {"died": on_died, "drawn": on_drawn}
MOVES = {"noop": {"apply": lambda s, c: s}}
`)
	if err != nil {
		t.Fatalf("LoadSpec: %v", err)
	}
	if len(s.Hooks) != 2 || s.Hooks[0].Type != "died" || s.Hooks[1].Type != "drawn" {
		t.Fatalf("hooks wrong/!ordered: %#v", s.Hooks)
	}
}

func TestLoadSpec_HooksRequireCCG(t *testing.T) {
	_, err := LoadSpec(`
META = {"name":"h","min_players":1,"max_players":1}
def setup(ctx): return {}
def end_if(state, ctx): return None
def legal_moves(state, ctx): return []
def on_died(event, ctx): pass
HOOKS = {"died": on_died}
MOVES = {"noop": {"apply": lambda s, c: s}}
`)
	if err == nil {
		t.Fatal("expected HOOKS without ccg in MODULES to be rejected")
	}
}
