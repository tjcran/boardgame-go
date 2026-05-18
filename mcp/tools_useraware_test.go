package mcp

import (
	"context"
	"testing"

	"github.com/tjcran/boardgame-go/games/tictactoe"
	"github.com/tjcran/boardgame-go/match"
	"github.com/tjcran/boardgame-go/storage"
)

func TestListGames_UserAware(t *testing.T) {
	mgr := match.NewManager(storage.NewMemory())
	mgr.MustRegister(tictactoe.New())
	store := NewInMemoryUserGames()
	reg := NewUserAwareRegistry(mgr, store)
	_ = store.Put(context.Background(), UserGame{UserID: "alice", Name: "z-game", Source: `
META = {"name":"z-game","min_players":2,"max_players":2}
def setup(ctx): return {}
MOVES = {"noop": {"args":[], "apply": lambda s, c: s}}
def end_if(state, ctx): return None
def legal_moves(state, ctx): return []
`})
	_ = reg.ReplayFromStore(context.Background())

	t.Run("alice sees built-in plus her game", func(t *testing.T) {
		tools := &Tools{Manager: mgr, Registry: reg}
		ctx := contextWithUser(context.Background(), "alice")
		out, err := tools.ListGames(ctx)
		if err != nil {
			t.Fatalf("ListGames alice: %v", err)
		}
		if len(out.Games) != 2 {
			t.Fatalf("alice ListGames: want 2 games, got %v", out.Games)
		}
	})
	t.Run("bob sees only built-in", func(t *testing.T) {
		tools := &Tools{Manager: mgr, Registry: reg}
		ctx := contextWithUser(context.Background(), "bob")
		out, err := tools.ListGames(ctx)
		if err != nil {
			t.Fatalf("ListGames bob: %v", err)
		}
		if len(out.Games) != 1 {
			t.Fatalf("bob ListGames: want 1 game, got %v", out.Games)
		}
	})
}

// TestCreateMatch_UserAware verifies that CreateMatch translates a public
// game name through the Registry to the Manager key when Registry is set,
// and that the match is created successfully for a user-owned game.
func TestCreateMatch_UserAware(t *testing.T) {
	mgr := match.NewManager(storage.NewMemory())
	mgr.MustRegister(tictactoe.New())
	store := NewInMemoryUserGames()
	reg := NewUserAwareRegistry(mgr, store)
	const src = `
META = {"name":"my-game","min_players":2,"max_players":2}
def setup(ctx): return {}
MOVES = {"noop": {"args":[], "apply": lambda s, c: s}}
def end_if(state, ctx): return None
def legal_moves(state, ctx): return []
`
	_ = store.Put(context.Background(), UserGame{UserID: "alice", Name: "my-game", Source: src})
	_ = reg.ReplayFromStore(context.Background())

	tools := &Tools{Manager: mgr, Registry: reg}
	ctx := contextWithUser(context.Background(), "alice")

	// Alice can create a match for her user-designed game using the public name.
	res, err := tools.CreateMatch(ctx, CreateMatchArgs{Game: "my-game", NumPlayers: 2})
	if err != nil {
		t.Fatalf("CreateMatch alice my-game: %v", err)
	}
	if res.MatchID == "" {
		t.Fatal("CreateMatch returned empty MatchID")
	}

	// Bob cannot create a match for alice's game.
	ctxBob := contextWithUser(context.Background(), "bob")
	_, err = tools.CreateMatch(ctxBob, CreateMatchArgs{Game: "my-game", NumPlayers: 2})
	if err == nil {
		t.Fatal("expected error for bob creating alice's game, got nil")
	}
}

// TestGetState_ReturnsPublicGameName verifies that GetState.Game is the
// public name (e.g. "hex"), not the internal manager key
// ("usergame:alice:hex"), for user-designed games.
func TestGetState_ReturnsPublicGameName(t *testing.T) {
	mgr := match.NewManager(storage.NewMemory())
	store := NewInMemoryUserGames()
	reg := NewUserAwareRegistry(mgr, store)
	const src = `
META = {"name":"hex","min_players":2,"max_players":2}
def setup(ctx): return {"cells": []}
MOVES = {"noop": {"args":[], "apply": lambda s, c: s}}
def end_if(state, ctx): return None
def legal_moves(state, ctx): return []
`
	if err := reg.RegisterUserGame(context.Background(), "alice", src, ""); err != nil {
		t.Fatalf("RegisterUserGame: %v", err)
	}
	tools := &Tools{Manager: mgr, Registry: reg}
	ctx := contextWithUser(context.Background(), "alice")
	cm, err := tools.CreateMatch(ctx, CreateMatchArgs{Game: "hex", NumPlayers: 2})
	if err != nil {
		t.Fatalf("CreateMatch: %v", err)
	}
	if cm.Game != "hex" {
		t.Errorf("CreateMatch.Game = %q, want hex", cm.Game)
	}

	gs, err := tools.GetState(ctx, GetStateArgs{MatchID: cm.MatchID})
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if gs.Game != "hex" {
		t.Errorf("GetState.Game = %q, want hex (got the internal manager key?)", gs.Game)
	}
}

// TestListGames_NoRegistry ensures that without a Registry, ListGames still
// works and does not expose usergame-prefixed Manager keys.
func TestListGames_NoRegistry(t *testing.T) {
	tools, _ := newToolsForTest(t)
	out, err := tools.ListGames(context.Background())
	if err != nil {
		t.Fatalf("ListGames: %v", err)
	}
	if len(out.Games) != 1 || out.Games[0].Name != "tic-tac-toe" {
		t.Fatalf("unexpected games without Registry: %v", out.Games)
	}
}
