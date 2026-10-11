package match

import (
	"errors"
	"testing"

	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/storage"
)

// TestFailedMoveLeavesStoredMatchPlayable: the in-memory store hands the
// manager a match whose State shares its maps with the stored copy, so a
// rejected move must not write into them. Here the move ends the
// player's stage and then its cascade fails; the stored match must still
// list the player as active, and the player must still be able to move.
func TestFailedMoveLeavesStoredMatchPlayable(t *testing.T) {
	type state struct{ N int }
	errBoom := errors.New("boom")
	game := &core.Game{
		Name: "failed-move-store", MinPlayers: 2, MaxPlayers: 2,
		Setup: func(_ core.Ctx, _ any) core.G { return &state{} },
		Moves: map[string]any{
			"enter": core.MoveFn(func(mc *core.MoveContext, _ ...any) (core.G, error) {
				mc.Events.SetActivePlayers(core.ActivePlayersConfig{CurrentPlayer: core.Stage("work")})
				return &state{N: mc.G.(*state).N + 1}, nil
			}),
			"endAndFail": core.MoveFn(func(mc *core.MoveContext, _ ...any) (core.G, error) {
				mc.Events.EndStage()
				mc.Queue.Push(mc.PlayerID, "boom")
				return &state{N: mc.G.(*state).N + 1}, nil
			}),
			"boom": core.MoveFn(func(*core.MoveContext, ...any) (core.G, error) {
				return nil, errBoom
			}),
			"work": core.MoveFn(func(mc *core.MoveContext, _ ...any) (core.G, error) {
				return &state{N: mc.G.(*state).N + 1}, nil
			}),
		},
		Turn: &core.TurnConfig{Stages: map[string]*core.StageConfig{"work": {}}},
	}
	store := storage.NewMemory()
	m := NewManager(store)
	m.MustRegister(game)
	id, _ := m.Create(game.Name, CreateOptions{})
	alice, _ := m.Join(id, "alice", JoinOptions{})
	_, _ = m.Join(id, "bob", JoinOptions{})
	if _, err := m.Move(id, alice.PlayerID, alice.PlayerCredentials, "enter", nil); err != nil {
		t.Fatalf("enter: %v", err)
	}

	if _, err := m.Move(id, alice.PlayerID, alice.PlayerCredentials, "endAndFail", nil); !errors.Is(err, errBoom) {
		t.Fatalf("endAndFail: err = %v, want boom", err)
	}
	stored, err := store.Get(id)
	if err != nil {
		t.Fatalf("store.Get: %v", err)
	}
	if got := stored.State.Ctx.ActivePlayers["0"]; got != "work" {
		t.Fatalf("stored ActivePlayers = %v, want seat 0 still in work", stored.State.Ctx.ActivePlayers)
	}
	if _, err := m.Move(id, alice.PlayerID, alice.PlayerCredentials, "work", nil); err != nil {
		t.Fatalf("move after the failed one: %v", err)
	}
}
