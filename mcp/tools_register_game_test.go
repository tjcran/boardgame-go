package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/tjcran/boardgame-go/games/tictactoe"
	"github.com/tjcran/boardgame-go/match"
	"github.com/tjcran/boardgame-go/storage"
)

func TestRegisterGame_HappyPath(t *testing.T) {
	mgr := match.NewManager(storage.NewMemory())
	mgr.MustRegister(tictactoe.New())
	reg := NewUserAwareRegistry(mgr, NewInMemoryUserGames())
	tools := &Tools{Manager: mgr, Registry: reg}

	const src = `
META = {"name":"hex","min_players":2,"max_players":2}
def setup(ctx): return {}
MOVES = {"pass": {"args":[], "apply": lambda s, c: s}}
def end_if(state, ctx): return None
def legal_moves(state, ctx): return []
`
	ctx := contextWithUser(context.Background(), "alice")
	out, err := tools.RegisterGame(ctx, RegisterGameArgs{Source: src, LLMGuide: "Play center."})
	if err != nil {
		t.Fatalf("RegisterGame: %v", err)
	}
	if out.Name != "hex" {
		t.Errorf("Name = %q", out.Name)
	}

	list, _ := tools.ListGames(ctx)
	found := false
	for _, g := range list.Games {
		if g.Name == "hex" {
			found = true
		}
	}
	if !found {
		t.Errorf("hex not in alice's list_games")
	}
}

func TestRegisterGame_RejectsCollisionWithBuiltin(t *testing.T) {
	mgr := match.NewManager(storage.NewMemory())
	mgr.MustRegister(tictactoe.New())
	reg := NewUserAwareRegistry(mgr, NewInMemoryUserGames())
	tools := &Tools{Manager: mgr, Registry: reg}

	const src = `
META = {"name":"tic-tac-toe","min_players":2,"max_players":2}
def setup(ctx): return {}
MOVES = {"pass": {"args":[], "apply": lambda s, c: s}}
def end_if(state, ctx): return None
def legal_moves(state, ctx): return []
`
	ctx := contextWithUser(context.Background(), "alice")
	_, err := tools.RegisterGame(ctx, RegisterGameArgs{Source: src})
	if err == nil || !strings.Contains(err.Error(), "collides") {
		t.Fatalf("expected collision error, got %v", err)
	}
}

func TestRegisterGame_RejectsBrokenSpec(t *testing.T) {
	mgr := match.NewManager(storage.NewMemory())
	reg := NewUserAwareRegistry(mgr, NewInMemoryUserGames())
	tools := &Tools{Manager: mgr, Registry: reg}
	ctx := contextWithUser(context.Background(), "alice")
	_, err := tools.RegisterGame(ctx, RegisterGameArgs{Source: `nonsense ===`})
	if err == nil {
		t.Fatalf("expected parse error")
	}
}
