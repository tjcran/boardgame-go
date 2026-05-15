package mcp

import (
	"context"
	"testing"

	"github.com/tjcran/boardgame-go/match"
	"github.com/tjcran/boardgame-go/storage"
)

func TestDeleteGame(t *testing.T) {
	mgr := match.NewManager(storage.NewMemory())
	reg := NewUserAwareRegistry(mgr, NewInMemoryUserGames())
	tools := &Tools{Manager: mgr, Registry: reg}
	ctx := contextWithUser(context.Background(), "alice")
	const src = `
META = {"name":"x","min_players":2,"max_players":2}
def setup(ctx): return {}
MOVES = {"noop": {"args":[], "apply": lambda s, c: None}}
def end_if(state, ctx): return None
def legal_moves(state, ctx): return []
`
	_, _ = tools.RegisterGame(ctx, RegisterGameArgs{Source: src})
	if _, err := tools.DeleteGame(ctx, DeleteGameArgs{Name: "x"}); err != nil {
		t.Fatalf("DeleteGame: %v", err)
	}
	list, _ := tools.ListGames(ctx)
	for _, g := range list.Games {
		if g.Name == "x" {
			t.Fatalf("x still in list after delete")
		}
	}
}

func TestDeleteGame_WrongOwner(t *testing.T) {
	mgr := match.NewManager(storage.NewMemory())
	reg := NewUserAwareRegistry(mgr, NewInMemoryUserGames())
	tools := &Tools{Manager: mgr, Registry: reg}
	const src = `
META = {"name":"x","min_players":2,"max_players":2}
def setup(ctx): return {}
MOVES = {"noop": {"args":[], "apply": lambda s, c: None}}
def end_if(state, ctx): return None
def legal_moves(state, ctx): return []
`
	_, _ = tools.RegisterGame(contextWithUser(context.Background(), "alice"), RegisterGameArgs{Source: src})
	_, err := tools.DeleteGame(contextWithUser(context.Background(), "bob"), DeleteGameArgs{Name: "x"})
	if err == nil {
		t.Fatalf("expected error; bob deleted alice's game")
	}
}
