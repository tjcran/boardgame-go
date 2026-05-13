package match

import (
	"strings"
	"testing"

	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/storage"
)

// versionedGame returns a game at schema version `to`. The Migrate fn
// rewrites G's payload to {Version: vNext}.
func versionedGame(to int) *core.Game {
	return &core.Game{
		Name:          "versioned",
		MinPlayers:    1,
		MaxPlayers:    1,
		Setup:         func(_ core.Ctx, _ any) core.G { return map[string]any{"v": to} },
		SchemaVersion: to,
		Migrate: func(s core.State, from int) (core.State, error) {
			s.G = map[string]any{"v": from + 1}
			return s, nil
		},
		Moves: map[string]any{"noop": core.MoveFn(func(mc *core.MoveContext, _ ...any) (core.G, error) {
			return mc.G, nil
		})},
		Turn: &core.TurnConfig{MinMoves: 1, MaxMoves: 1},
	}
}

func TestMigrationRunsOnState(t *testing.T) {
	m := NewManager(storage.NewMemory())
	m.MustRegister(versionedGame(1))
	id, err := m.Create("versioned", CreateOptions{})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Bump the game's schema to v3 — older versions of the in-memory
	// match must migrate up to v3 on next load.
	m = NewManager(storage.NewMemory())
	m.MustRegister(versionedGame(3))
	_ = m.store.Create(&storage.Match{
		ID: id, GameName: "versioned",
		State:         core.State{G: map[string]any{"v": 1}},
		Players:       []storage.Player{{ID: "0", Seat: "0", Name: "alice"}},
		SchemaVersion: 1,
	})
	got, err := m.State(id)
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	if got.SchemaVersion != 3 {
		t.Fatalf("expected SchemaVersion=3 after migration, got %d", got.SchemaVersion)
	}
	if got.State.G.(map[string]any)["v"] != 3 {
		t.Fatalf("expected v=3 after migration chain, got %+v", got.State.G)
	}
}

func TestMigrationMissingFunctionErrors(t *testing.T) {
	m := NewManager(storage.NewMemory())
	g := versionedGame(2)
	g.Migrate = nil // intentionally missing
	m.MustRegister(g)
	_ = m.store.Create(&storage.Match{
		ID: "x", GameName: "versioned",
		State:         core.State{G: map[string]any{"v": 0}},
		Players:       []storage.Player{},
		SchemaVersion: 0,
	})
	_, err := m.State("x")
	if err == nil || !strings.Contains(err.Error(), "Migrate fn") {
		t.Fatalf("expected missing-Migrate error, got %v", err)
	}
}
