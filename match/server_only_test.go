package match

import (
	"context"
	"errors"
	"testing"

	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/storage"
)

// serverOnlyTablesGame registers ServerOnly moves in every kind of move
// table. "enter" gates the current player into the "admin" stage.
//
//   - "globalAdmin" is ServerOnly in the game-level table.
//   - "stageAdmin" is ServerOnly in the stage table only.
//   - "shadowed" is ServerOnly at game level, but the stage table
//     registers an ordinary move under the same name, so a player in the
//     stage runs the stage's move.
//
// withPhase moves the top-level moves into a start phase's table, which
// replaces the game-level table for the whole match.
func serverOnlyTablesGame(withPhase bool) *core.Game {
	type state struct{ Moves []string }
	record := func(name string) core.MoveFn {
		return func(mc *core.MoveContext, _ ...any) (core.G, error) {
			prev := mc.G.(*state)
			return &state{Moves: append(append([]string(nil), prev.Moves...), name)}, nil
		}
	}
	serverOnly := func(name string) core.Move {
		return core.Move{ServerOnly: true, Move: record(name)}
	}
	top := map[string]any{
		"enter": core.MoveFn(func(mc *core.MoveContext, args ...any) (core.G, error) {
			mc.Events.SetActivePlayers(core.ActivePlayersConfig{CurrentPlayer: core.Stage("admin")})
			return record("enter")(mc, args...)
		}),
		"globalAdmin": serverOnly("globalAdmin"),
		"shadowed":    serverOnly("shadowed"),
	}
	game := &core.Game{
		Name: "server-only-tables", MinPlayers: 2, MaxPlayers: 2,
		Setup: func(_ core.Ctx, _ any) core.G { return &state{} },
		Turn: &core.TurnConfig{
			Stages: map[string]*core.StageConfig{
				"admin": {Moves: map[string]any{
					"stageAdmin": serverOnly("stageAdmin"),
					"shadowed":   record("shadowed"),
				}},
			},
		},
	}
	if withPhase {
		game.Phases = map[string]*core.PhaseConfig{"main": {Start: true, Moves: top}}
	} else {
		game.Moves = top
	}
	return game
}

// TestServerOnlyGuardCoversEveryMoveTable: a credentialed client may not
// run a ServerOnly move wherever it is registered — game, phase or stage
// table — and the guard judges the move the reducer would actually run,
// so a stage move that shadows a ServerOnly name stays playable. DryMove
// is refused the same way, the refusal is reported like any rejected
// move, and DispatchServer still runs the move.
func TestServerOnlyGuardCoversEveryMoveTable(t *testing.T) {
	cases := []struct {
		name      string
		withPhase bool
		enter     bool // gate the player into the "admin" stage first
		move      string
		wantErr   error
	}{
		{name: "game table", move: "globalAdmin", wantErr: ErrServerOnly},
		{name: "phase table", withPhase: true, move: "globalAdmin", wantErr: ErrServerOnly},
		{name: "stage table", enter: true, move: "stageAdmin", wantErr: ErrServerOnly},
		{name: "stage table inside a phase", withPhase: true, enter: true, move: "stageAdmin", wantErr: ErrServerOnly},
		{name: "outside the stage, a ServerOnly name is refused", move: "shadowed", wantErr: ErrServerOnly},
		{name: "inside the stage, its ordinary move shadows it", enter: true, move: "shadowed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			game := serverOnlyTablesGame(tc.withPhase)
			m := NewManager(storage.NewMemory())
			m.MustRegister(game)
			var rejected []error
			m.OnLifecycleKind(LifecycleMatchMoveRejected, func(ev LifecycleEvent) {
				rejected = append(rejected, ev.Err)
			})
			id, _ := m.Create(game.Name, CreateOptions{})
			alice, _ := m.Join(id, "alice", JoinOptions{})
			_, _ = m.Join(id, "bob", JoinOptions{})
			if tc.enter {
				if _, err := m.Move(id, alice.PlayerID, alice.PlayerCredentials, "enter", nil); err != nil {
					t.Fatalf("enter: %v", err)
				}
			}

			_, dryErr := m.DryMove(id, alice.PlayerID, alice.PlayerCredentials, tc.move, nil)
			_, err := m.Move(id, alice.PlayerID, alice.PlayerCredentials, tc.move, nil)
			if tc.wantErr == nil {
				if dryErr != nil || err != nil {
					t.Fatalf("%s: DryMove err = %v, Move err = %v, want both nil", tc.move, dryErr, err)
				}
				return
			}
			if !errors.Is(dryErr, tc.wantErr) {
				t.Errorf("DryMove %s: err = %v, want %v", tc.move, dryErr, tc.wantErr)
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Move %s: err = %v, want %v", tc.move, err, tc.wantErr)
			}
			if len(rejected) != 1 || !errors.Is(rejected[0], tc.wantErr) {
				t.Errorf("rejected-move lifecycle events = %v, want one %v", rejected, tc.wantErr)
			}
			if _, err := m.DispatchServer(context.Background(), id, "0", tc.move); err != nil {
				t.Fatalf("DispatchServer %s: %v", tc.move, err)
			}
		})
	}
}
