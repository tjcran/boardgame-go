package core

import (
	"errors"
	"reflect"
	"testing"
)

// stateView copies the parts of a State a move can change, so a test can
// compare a state against how it looked before Apply ran even if Apply
// wrote through a map or slice the two share.
type stateView struct {
	StateID       int
	NowMs         int64
	Events        []string
	Blocks        []BlockSpec
	ActivePlayers map[string]string
	MoveCounts    map[string]int
	StageMin      map[string]int
	StageMax      map[string]int
	PlayOrder     []string
	ActiveStack   []activeFrame
	Log           []LogEntry
	Queue         []QueuedAction
}

func viewOf(s State) stateView {
	v := stateView{
		StateID:       s.StateID,
		NowMs:         s.Ctx.NowMs,
		Events:        append([]string(nil), s.G.(*targetState).Events...),
		Blocks:        append([]BlockSpec(nil), s.Blocks...),
		ActivePlayers: copyStrMap(s.Ctx.ActivePlayers),
		MoveCounts:    copyIntMap(s.MoveCounts),
		StageMin:      copyIntMap(s.StageMinMoves),
		StageMax:      copyIntMap(s.StageMaxMoves),
		PlayOrder:     append([]string(nil), s.Ctx.PlayOrder...),
		Log:           append([]LogEntry(nil), s.Log...),
		Queue:         append([]QueuedAction(nil), s.Queue...),
	}
	for _, f := range s.ActiveStack {
		v.ActiveStack = append(v.ActiveStack, activeFrame{
			ActivePlayers: copyStrMap(f.ActivePlayers),
			MoveCounts:    copyIntMap(f.MoveCounts),
			StageMin:      copyIntMap(f.StageMin),
			StageMax:      copyIntMap(f.StageMax),
		})
	}
	return v
}

var errBoom = errors.New("boom")

// rollbackGame gates the current player into the "work" stage, where
// every move counts against the stage's move counter.
//
//   - "enter" gates the player into "work" and raises a prompt addressed
//     to them; "answer" answers it.
//   - "nest" moves the player into "inner", saving "work" to revert to.
//   - "failAnswer" consumes the prompt and then fails.
//   - "endAndFail" ends the player's stage, eliminates the next seat in
//     play order and queues "boom", a cascade step that fails.
//   - "revertAndFail" ends the player's stage, so the saved "work" set
//     is restored, moves them on to "again", and fails the same way.
func rollbackGame() *Game {
	fail := func(name string, events func(mc *MoveContext)) MoveFn {
		return func(mc *MoveContext, _ ...any) (G, error) {
			events(mc)
			mc.Queue.Push(mc.PlayerID, "boom")
			return cloneT(mc.G.(*targetState), name), nil
		}
	}
	return &Game{
		Name:       "rollback-test",
		MinPlayers: 2,
		MaxPlayers: 3,
		Setup:      func(_ Ctx, _ any) G { return &targetState{} },
		Moves: map[string]any{
			"enter": MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
				// MaxMoves only populates the stage move limits; no
				// test makes that many moves.
				mc.Events.SetActivePlayers(ActivePlayersConfig{CurrentPlayer: Stage("work"), MaxMoves: 10})
				mc.Queue.Block("confirm", mc.PlayerID, nil)
				return cloneT(mc.G.(*targetState), "enter"), nil
			}),
			"answer": recordMove("answer"),
			"nest": MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
				mc.Events.SetActivePlayers(ActivePlayersConfig{CurrentPlayer: Stage("inner"), Revert: true})
				return cloneT(mc.G.(*targetState), "nest"), nil
			}),
			"failAnswer": MoveFn(func(*MoveContext, ...any) (G, error) {
				return nil, errBoom
			}),
			"endAndFail": fail("endAndFail", func(mc *MoveContext) {
				mc.Events.EndStage()
				mc.Events.RemovePlayer(mc.Ctx.PlayOrder[1])
			}),
			"revertAndFail": fail("revertAndFail", func(mc *MoveContext) {
				mc.Events.EndStage()
				mc.Events.SetStage("again")
			}),
			"boom": MoveFn(func(*MoveContext, ...any) (G, error) {
				return nil, errBoom
			}),
		},
		Turn: &TurnConfig{Stages: map[string]*StageConfig{"work": {}, "inner": {}, "again": {}}},
	}
}

// TestFailedApplyReturnsPreMoveState: whatever fails after a move
// starts, Apply returns the state it was given and leaves that state as
// it was. Neither the consumed prompt, the request's NowMs, the stage
// bookkeeping, the play order nor the saved active-player sets may leak
// out of a failed move, and a successful move must not write through to
// its input either.
func TestFailedApplyReturnsPreMoveState(t *testing.T) {
	game := rollbackGame()
	// setup returns a three-seat match whose current player is in the
	// "work" stage with one counted move behind them, a prompt pending,
	// and, with nest, an "inner" stage over a saved "work" set.
	setup := func(t *testing.T, nest bool) (State, string) {
		t.Helper()
		state := NewMatch(game, 3, nil)
		player := state.Ctx.CurrentPlayer
		apply := func(req MoveRequest) {
			t.Helper()
			req.PlayerID = player
			var err error
			if state, err = Apply(game, state, req); err != nil {
				t.Fatalf("%s: %v", req.Move, err)
			}
		}
		apply(MoveRequest{Move: "enter", NowMs: 100})
		apply(MoveRequest{Move: "answer", ResumeTag: "confirm", NowMs: 150})
		if nest {
			apply(MoveRequest{Move: "nest", NowMs: 175})
		}
		if state.MoveCounts[player] == 0 && !nest {
			t.Fatalf("MoveCounts = %v, want a count for %s", state.MoveCounts, player)
		}
		// Raise a prompt for a failing move to consume.
		state.Blocks = []BlockSpec{{Tag: "confirm", PlayerID: player}}
		return state, player
	}

	cases := []struct {
		name    string
		nest    bool
		req     MoveRequest
		wantErr error
	}{
		{
			name:    "unknown ResumeTag",
			req:     MoveRequest{Move: "failAnswer", ResumeTag: "nope", NowMs: 200},
			wantErr: ErrUnknownResumeTag,
		},
		{
			name:    "move fails after consuming a prompt",
			req:     MoveRequest{Move: "failAnswer", ResumeTag: "confirm", NowMs: 200},
			wantErr: errBoom,
		},
		{
			name:    "cascade fails after the move ended a stage and removed a player",
			req:     MoveRequest{Move: "endAndFail", ResumeTag: "confirm", NowMs: 200},
			wantErr: errBoom,
		},
		{
			name:    "cascade fails after the move restored a saved active set",
			nest:    true,
			req:     MoveRequest{Move: "revertAndFail", ResumeTag: "confirm", NowMs: 200},
			wantErr: errBoom,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state, player := setup(t, tc.nest)
			before := viewOf(state)
			req := tc.req
			req.PlayerID = player
			next, err := Apply(game, state, req)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if got := viewOf(next); !reflect.DeepEqual(got, before) {
				t.Errorf("returned state is not the pre-move state:\n got %+v\nwant %+v", got, before)
			}
			if got := viewOf(state); !reflect.DeepEqual(got, before) {
				t.Errorf("failed move changed the caller's state:\n got %+v\nwant %+v", got, before)
			}
		})
	}

	t.Run("successful move leaves its input alone", func(t *testing.T) {
		state, player := setup(t, false)
		before := viewOf(state)
		next, err := Apply(game, state, MoveRequest{PlayerID: player, Move: "answer", ResumeTag: "confirm", NowMs: 300})
		if err != nil {
			t.Fatalf("answer: %v", err)
		}
		if next.MoveCounts[player] != before.MoveCounts[player]+1 {
			t.Fatalf("next MoveCounts = %v, want one more than %v", next.MoveCounts, before.MoveCounts)
		}
		if got := viewOf(state); !reflect.DeepEqual(got, before) {
			t.Errorf("successful move changed the caller's state:\n got %+v\nwant %+v", got, before)
		}
	})
}
