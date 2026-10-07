package core

import (
	"errors"
	"testing"
)

// recordMove is a move that appends its own name to targetState.Events,
// so a test can tell which move actually ran.
func recordMove(name string) MoveFn {
	return func(mc *MoveContext, _ ...any) (G, error) {
		return cloneT(mc.G.(*targetState), name), nil
	}
}

// exclusiveStageGame models a response window: the current player
// gates themselves into a "respond" stage whose table holds the only
// moves a response may be. respondExclusive toggles StageConfig.Exclusive
// on that stage so the same game pins both the opt-in and the default.
//
//   - "enter" gates the current player into "respond".
//   - "enterAndAsk" does the same and raises a target prompt addressed to
//     that player; "answer" (top-level only) is the resume move.
//   - "lock" gates the current player into "locked", an Exclusive stage
//     with no move table of its own.
//   - "phaseMove" is an ordinary top-level move — the one an exclusive
//     stage must refuse.
//   - "concede" is an AnyPlayer top-level move.
func exclusiveStageGame(respondExclusive bool) *Game {
	gate := func(stage, name string, ask bool) MoveFn {
		return func(mc *MoveContext, _ ...any) (G, error) {
			mc.Events.SetActivePlayers(ActivePlayersConfig{CurrentPlayer: Stage(stage)})
			if ask {
				mc.Queue.RequestTarget(mc.PlayerID, TargetRequest{
					Kind: "pick", Candidates: []any{10, 20}, Min: 1, Max: 1,
				})
			}
			return cloneT(mc.G.(*targetState), name), nil
		}
	}
	return &Game{
		Name:       "exclusive-stage-test",
		MinPlayers: 2,
		MaxPlayers: 2,
		Setup:      func(_ Ctx, _ any) G { return &targetState{} },
		Moves: map[string]any{
			"enter":       gate("respond", "enter", false),
			"enterAndAsk": gate("respond", "enterAndAsk", true),
			"lock":        gate("locked", "lock", false),
			"phaseMove":   recordMove("phaseMove"),
			"answer":      recordMove("answer"),
			"concede":     Move{AnyPlayer: true, Move: recordMove("concede")},
		},
		Turn: &TurnConfig{
			Stages: map[string]*StageConfig{
				"respond": {
					Exclusive: respondExclusive,
					Moves:     map[string]any{"respond": recordMove("respond")},
				},
				"locked": {Exclusive: true},
			},
		},
	}
}

// TestExclusiveStage pins what a player gated into a stage may dispatch.
// An Exclusive stage refuses every move outside its own table except the
// two classes the engine must always accept: AnyPlayer moves (concede /
// forfeit / timeout) and the answer to a prompt addressed to the caller.
// A stage without Exclusive keeps falling through to the top-level table.
func TestExclusiveStage(t *testing.T) {
	cases := []struct {
		name      string
		exclusive bool
		open      string      // move the current player makes to enter the stage
		req       MoveRequest // made by that same, now-gated player
		wantErr   error       // nil = the move must run
	}{
		{
			name: "exclusive stage rejects a top-level move", exclusive: true,
			open: "enter", req: MoveRequest{Move: "phaseMove"}, wantErr: ErrMoveNotInStage,
		},
		{
			name: "exclusive stage allows its own moves", exclusive: true,
			open: "enter", req: MoveRequest{Move: "respond"},
		},
		{
			name: "exclusive stage allows an AnyPlayer move", exclusive: true,
			open: "enter", req: MoveRequest{Move: "concede"},
		},
		{
			name: "exclusive stage allows answering the caller's own prompt", exclusive: true,
			open: "enterAndAsk", req: MoveRequest{Move: "answer", Args: []any{10}, ResumeTag: "pick"},
		},
		{
			name: "non-exclusive stage falls through to top-level moves", exclusive: false,
			open: "enter", req: MoveRequest{Move: "phaseMove"},
		},
		{
			name: "exclusive stage without a move table rejects top-level moves",
			open: "lock", req: MoveRequest{Move: "phaseMove"}, wantErr: ErrMoveNotInStage,
		},
		{
			name: "exclusive stage without a move table allows AnyPlayer moves",
			open: "lock", req: MoveRequest{Move: "concede"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			game := exclusiveStageGame(tc.exclusive)
			state := NewMatch(game, 2, nil)
			player := state.Ctx.CurrentPlayer

			state, err := Apply(game, state, MoveRequest{PlayerID: player, Move: tc.open})
			if err != nil {
				t.Fatalf("%s: %v", tc.open, err)
			}
			if _, ok := state.Ctx.ActivePlayers[player]; !ok {
				t.Fatalf("%s did not gate the player into a stage: ActivePlayers = %v",
					tc.open, state.Ctx.ActivePlayers)
			}

			req := tc.req
			req.PlayerID = player
			next, err := Apply(game, state, req)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("%s: err = %v, want %v", req.Move, err, tc.wantErr)
				}
				if next.StateID != state.StateID || len(next.G.(*targetState).Events) != len(state.G.(*targetState).Events) {
					t.Errorf("rejected move changed state: StateID %d -> %d, events %v -> %v",
						state.StateID, next.StateID, state.G.(*targetState).Events, next.G.(*targetState).Events)
				}
				return
			}
			if err != nil {
				t.Fatalf("%s: %v", req.Move, err)
			}
			events := next.G.(*targetState).Events
			if len(events) == 0 || events[len(events)-1] != req.Move {
				t.Errorf("%s did not run: events = %v", req.Move, events)
			}
		})
	}
}

// TestExclusiveStagePhaseOverridesGlobal: when a phase's Turn.Stages and
// the game-level Turn.Stages both define the caller's stage, the phase's
// Exclusive flag decides — the same precedence that picks which stage
// config's hooks run.
func TestExclusiveStagePhaseOverridesGlobal(t *testing.T) {
	cases := []struct {
		name            string
		phaseExclusive  bool
		globalExclusive bool
		wantErr         error
	}{
		{name: "exclusive phase stage over permissive global", phaseExclusive: true, wantErr: ErrMoveNotInStage},
		{name: "permissive phase stage over exclusive global", globalExclusive: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stages := func(exclusive bool) map[string]*StageConfig {
				return map[string]*StageConfig{
					"respond": {Exclusive: exclusive, Moves: map[string]any{"respond": recordMove("respond")}},
				}
			}
			game := &Game{
				Name:       "exclusive-stage-precedence-test",
				MinPlayers: 2,
				MaxPlayers: 2,
				Setup:      func(_ Ctx, _ any) G { return &targetState{} },
				Moves: map[string]any{
					"enter": MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
						mc.Events.SetActivePlayers(ActivePlayersConfig{CurrentPlayer: Stage("respond")})
						return cloneT(mc.G.(*targetState), "enter"), nil
					}),
					"phaseMove": recordMove("phaseMove"),
				},
				Phases: map[string]*PhaseConfig{
					"main": {Start: true, Turn: &TurnConfig{Stages: stages(tc.phaseExclusive)}},
				},
				Turn: &TurnConfig{Stages: stages(tc.globalExclusive)},
			}
			state := NewMatch(game, 2, nil)
			player := state.Ctx.CurrentPlayer
			if state.Ctx.Phase != "main" {
				t.Fatalf("phase = %q, want main", state.Ctx.Phase)
			}

			state, err := Apply(game, state, MoveRequest{PlayerID: player, Move: "enter"})
			if err != nil {
				t.Fatalf("enter: %v", err)
			}
			_, err = Apply(game, state, MoveRequest{PlayerID: player, Move: "phaseMove"})
			if tc.wantErr == nil && err != nil {
				t.Fatalf("phaseMove: %v", err)
			}
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Fatalf("phaseMove: err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}
