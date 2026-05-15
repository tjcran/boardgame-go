package mcp

import (
	"context"
	"testing"

	"github.com/tjcran/boardgame-go/games/tictactoe"
	"github.com/tjcran/boardgame-go/match"
	"github.com/tjcran/boardgame-go/storage"
)

func newRegistryWithBuiltins(t *testing.T) (*match.Manager, UserGameStore, *UserAwareRegistry) {
	mgr := match.NewManager(storage.NewMemory())
	mgr.MustRegister(tictactoe.New())
	store := NewInMemoryUserGames()
	reg := NewUserAwareRegistry(mgr, store)
	return mgr, store, reg
}

func TestListGames_BuiltinsOnly(t *testing.T) {
	_, _, reg := newRegistryWithBuiltins(t)
	names, err := reg.ListForUser(context.Background(), "alice")
	if err != nil { t.Fatalf("ListForUser: %v", err) }
	if len(names) != 1 || names[0].Name != "tic-tac-toe" {
		t.Fatalf("expected only tic-tac-toe, got %v", names)
	}
}

func TestListGames_IncludesOwnedUserGame(t *testing.T) {
	mgr, store, reg := newRegistryWithBuiltins(t)
	const src = `
META = {"name":"hex","min_players":2,"max_players":2}
def setup(ctx): return {}
MOVES = {"noop": {"args":[], "apply": lambda s, c: None}}
def end_if(state, ctx): return None
def legal_moves(state, ctx): return []
`
	_ = store.Put(context.Background(), UserGame{UserID: "alice", Name: "hex", Source: src})
	if err := reg.ReplayFromStore(context.Background()); err != nil {
		t.Fatalf("ReplayFromStore: %v", err)
	}
	names, _ := reg.ListForUser(context.Background(), "alice")
	if len(names) != 2 { t.Fatalf("ListForUser alice: %v", names) }

	// Bob should NOT see hex.
	bobNames, _ := reg.ListForUser(context.Background(), "bob")
	if len(bobNames) != 1 || bobNames[0].Name != "tic-tac-toe" {
		t.Fatalf("Bob saw alice's game: %v", bobNames)
	}

	// Lookup translates public name to Manager-key.
	managerKey, ownerID, err := reg.LookupForUser(context.Background(), "alice", "hex")
	if err != nil { t.Fatalf("LookupForUser: %v", err) }
	if ownerID != "alice" { t.Errorf("ownerID = %q", ownerID) }
	if mgr.Game(managerKey) == nil { t.Errorf("Manager missing key %q", managerKey) }
}

func TestLookup_BuiltinWinsOverUserName(t *testing.T) {
	mgr, store, reg := newRegistryWithBuiltins(t)
	// Try to shadow built-in with a user game named the same.
	_ = store.Put(context.Background(), UserGame{UserID: "alice", Name: "tic-tac-toe", Source: ""})
	_ = reg.ReplayFromStore(context.Background())

	key, _, err := reg.LookupForUser(context.Background(), "alice", "tic-tac-toe")
	if err != nil { t.Fatalf("LookupForUser: %v", err) }
	if key != "tic-tac-toe" { t.Errorf("expected built-in (key=tic-tac-toe), got %q", key) }
	_ = mgr
}

func TestRegisterAndLookupRoundTrip(t *testing.T) {
	_, _, reg := newRegistryWithBuiltins(t)
	const src = `
META = {"name":"foo","min_players":2,"max_players":2}
def setup(ctx): return {}
MOVES = {"noop": {"args":[], "apply": lambda s, c: None}}
def end_if(state, ctx): return None
def legal_moves(state, ctx): return []
`
	if err := reg.RegisterUserGame(context.Background(), "alice", src, "guide text"); err != nil {
		t.Fatalf("RegisterUserGame: %v", err)
	}
	key, _, err := reg.LookupForUser(context.Background(), "alice", "foo")
	if err != nil || key == "" { t.Fatalf("LookupForUser: %v %q", err, key) }
}
