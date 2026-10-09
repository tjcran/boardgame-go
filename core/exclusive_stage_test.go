package core

import (
	"errors"
	"reflect"
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
//     that player that names "answer" (top-level only) as its answer.
//   - "enterAndAskUnnamed" raises two prompts, "pick" then "confirm",
//     that name no answer move, so any move carrying a tag consumes one.
//   - "lock" gates the current player into "locked", an Exclusive stage
//     with no move table of its own.
//   - "phaseMove" is an ordinary top-level move — the one an exclusive
//     stage must refuse.
//   - "concede" is an AnyPlayer top-level move; "forfeit" is an
//     IgnoreBlocks top-level move.
func exclusiveStageGame(respondExclusive bool) *Game {
	type ask int
	const (
		askNone ask = iota
		askNamed
		askUnnamed
	)
	gate := func(stage, name string, prompt ask) MoveFn {
		return func(mc *MoveContext, _ ...any) (G, error) {
			mc.Events.SetActivePlayers(ActivePlayersConfig{CurrentPlayer: Stage(stage)})
			pick := TargetRequest{Kind: "pick", Candidates: []any{10, 20}, Min: 1, Max: 1}
			switch prompt {
			case askNamed:
				mc.Queue.RequestTarget(mc.PlayerID, pick, AnsweredBy("answer"))
			case askUnnamed:
				mc.Queue.RequestTarget(mc.PlayerID, pick)
				mc.Queue.Block("confirm", mc.PlayerID, nil)
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
			"enter":              gate("respond", "enter", askNone),
			"enterAndAsk":        gate("respond", "enterAndAsk", askNamed),
			"enterAndAskUnnamed": gate("respond", "enterAndAskUnnamed", askUnnamed),
			"lock":               gate("locked", "lock", askNone),
			"phaseMove":          recordMove("phaseMove"),
			"answer":             recordMove("answer"),
			"concede":            Move{AnyPlayer: true, Move: recordMove("concede")},
			"forfeit":            Move{IgnoreBlocks: true, Move: recordMove("forfeit")},
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
// classes the engine must always accept: AnyPlayer and IgnoreBlocks moves
// (concede / forfeit / timeout) and the named answer to a prompt
// addressed to the caller. A non-answer move carrying the prompt's tag is
// refused with the state untouched. A stage without Exclusive keeps
// falling through to the top-level table.
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
			name: "exclusive stage allows an IgnoreBlocks move", exclusive: true,
			open: "enter", req: MoveRequest{Move: "forfeit"},
		},
		{
			name: "exclusive stage allows an IgnoreBlocks move while a prompt is pending", exclusive: true,
			open: "enterAndAsk", req: MoveRequest{Move: "forfeit"},
		},
		{
			name: "exclusive stage allows the named answer to the caller's own prompt", exclusive: true,
			open: "enterAndAsk", req: MoveRequest{Move: "answer", Args: []any{10}, ResumeTag: "pick"},
		},
		{
			name: "named prompt rejects a top-level non-answer move carrying its tag", exclusive: true,
			open: "enterAndAsk", req: MoveRequest{Move: "phaseMove", ResumeTag: "pick"},
			wantErr: ErrUnknownResumeTag,
		},
		{
			name: "named prompt rejects a stage move carrying its tag", exclusive: true,
			open: "enterAndAsk", req: MoveRequest{Move: "respond", ResumeTag: "pick"},
			wantErr: ErrUnknownResumeTag,
		},
		{
			name: "unnamed prompt does not exempt a top-level move carrying its tag", exclusive: true,
			open: "enterAndAskUnnamed", req: MoveRequest{Move: "phaseMove", ResumeTag: "pick"},
			wantErr: ErrMoveNotInStage,
		},
		{
			name: "unnamed prompt's top-level answer is not exempt either", exclusive: true,
			open: "enterAndAskUnnamed", req: MoveRequest{Move: "answer", Args: []any{10}, ResumeTag: "pick"},
			wantErr: ErrMoveNotInStage,
		},
		{
			name: "unnamed prompt can be answered from the stage table", exclusive: true,
			open: "enterAndAskUnnamed", req: MoveRequest{Move: "respond", ResumeTag: "pick"},
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
		{
			name: "exclusive stage without a move table allows IgnoreBlocks moves",
			open: "lock", req: MoveRequest{Move: "forfeit"},
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
				assertUnchanged(t, state, next)
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

// assertUnchanged fails when a rejected move left any trace: the
// returned state must be the pre-move state, pending prompts and the
// caller's stage included.
func assertUnchanged(t *testing.T, before, after State) {
	t.Helper()
	if after.StateID != before.StateID {
		t.Errorf("rejected move changed StateID %d -> %d", before.StateID, after.StateID)
	}
	if b, a := before.G.(*targetState).Events, after.G.(*targetState).Events; !reflect.DeepEqual(a, b) {
		t.Errorf("rejected move ran: events %v -> %v", b, a)
	}
	if !reflect.DeepEqual(after.Blocks, before.Blocks) {
		t.Errorf("rejected move changed Blocks %+v -> %+v", before.Blocks, after.Blocks)
	}
	if !reflect.DeepEqual(after.Ctx.ActivePlayers, before.Ctx.ActivePlayers) {
		t.Errorf("rejected move changed ActivePlayers %v -> %v", before.Ctx.ActivePlayers, after.Ctx.ActivePlayers)
	}
}

// enterStage opens the named gate move for the current player and
// returns the state with that player inside the stage.
func enterStage(t *testing.T, game *Game, open string) (State, string) {
	t.Helper()
	state := NewMatch(game, 2, nil)
	player := state.Ctx.CurrentPlayer
	state, err := Apply(game, state, MoveRequest{PlayerID: player, Move: open})
	if err != nil {
		t.Fatalf("%s: %v", open, err)
	}
	if state.Ctx.ActivePlayers[player] == "" {
		t.Fatalf("%s did not gate the player into a stage: ActivePlayers = %v", open, state.Ctx.ActivePlayers)
	}
	return state, player
}

// TestExclusiveStageRejectedResumeKeepsEveryPrompt: a move that consumes
// one of several pending prompts and is then refused must hand back the
// prompts exactly as they were. Removing the consumed prompt in place
// would shift the shared backing array under the returned pre-move state.
func TestExclusiveStageRejectedResumeKeepsEveryPrompt(t *testing.T) {
	game := exclusiveStageGame(true)
	state, player := enterStage(t, game, "enterAndAskUnnamed")
	if len(state.Blocks) != 2 || state.Blocks[0].Tag != "pick" || state.Blocks[1].Tag != "confirm" {
		t.Fatalf("Blocks = %+v, want [pick confirm]", state.Blocks)
	}
	want := append([]BlockSpec(nil), state.Blocks...)

	next, err := Apply(game, state, MoveRequest{PlayerID: player, Move: "phaseMove", ResumeTag: "pick"})
	if !errors.Is(err, ErrMoveNotInStage) {
		t.Fatalf("err = %v, want ErrMoveNotInStage", err)
	}
	assertUnchanged(t, state, next)
	if !reflect.DeepEqual(state.Blocks, want) {
		t.Errorf("rejected resume corrupted the caller's Blocks: %+v, want %+v", state.Blocks, want)
	}
}

// TestNamedPromptOutOfTurnRejectsNonAnswer: a prompt that names its
// answer is consumable only by that move, so the out-of-turn exemption
// its addressee gets cannot carry an unrelated move.
func TestNamedPromptOutOfTurnRejectsNonAnswer(t *testing.T) {
	game := &Game{
		Name:       "named-prompt-off-turn-test",
		MinPlayers: 2,
		MaxPlayers: 2,
		Setup:      func(_ Ctx, _ any) G { return &targetState{} },
		Moves: map[string]any{
			"askOpponent": MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
				mc.Queue.RequestTarget(other(mc.PlayerID), TargetRequest{
					Kind: "pick", Candidates: []any{10, 20}, Min: 1, Max: 1,
				}, AnsweredBy("answer"))
				return cloneT(mc.G.(*targetState), "askOpponent"), nil
			}),
			"answer":    recordMove("answer"),
			"phaseMove": recordMove("phaseMove"),
		},
	}
	state := NewMatch(game, 2, nil)
	responder := other(state.Ctx.CurrentPlayer)
	state, err := Apply(game, state, MoveRequest{PlayerID: state.Ctx.CurrentPlayer, Move: "askOpponent"})
	if err != nil {
		t.Fatalf("askOpponent: %v", err)
	}
	if got := state.Blocks[0].Move; got != "answer" {
		t.Fatalf("Blocks[0].Move = %q, want answer", got)
	}

	next, err := Apply(game, state, MoveRequest{PlayerID: responder, Move: "phaseMove", ResumeTag: "pick"})
	if !errors.Is(err, ErrUnknownResumeTag) {
		t.Fatalf("non-answer move with the prompt's tag: err = %v, want ErrUnknownResumeTag", err)
	}
	assertUnchanged(t, state, next)

	if _, err := Apply(game, state, MoveRequest{PlayerID: responder, Move: "answer", Args: []any{10}, ResumeTag: "pick"}); err != nil {
		t.Fatalf("named answer out of turn: %v", err)
	}
}

// TestExclusiveStageServerDispatch: a move the server dispatches on its
// own authority is not confined by the stage's table, the log records
// that, and Replay re-applies it the same way. The seat must still be
// allowed to move.
func TestExclusiveStageServerDispatch(t *testing.T) {
	game := exclusiveStageGame(true)
	state, player := enterStage(t, game, "enter")

	if _, err := Apply(game, state, MoveRequest{PlayerID: player, Move: "phaseMove"}); !errors.Is(err, ErrMoveNotInStage) {
		t.Fatalf("client phaseMove: err = %v, want ErrMoveNotInStage", err)
	}
	next, err := Apply(game, state, MoveRequest{PlayerID: player, Move: "phaseMove", ServerDispatch: true})
	if err != nil {
		t.Fatalf("server-dispatched phaseMove: %v", err)
	}
	if events := next.G.(*targetState).Events; events[len(events)-1] != "phaseMove" {
		t.Fatalf("phaseMove did not run: events = %v", events)
	}
	last := next.Log[len(next.Log)-1]
	if last.Move != "phaseMove" || !last.ServerDispatch {
		t.Fatalf("last log entry = %+v, want phaseMove with ServerDispatch", last)
	}

	replayed, err := Replay(game, next.Log, 2, nil)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if !reflect.DeepEqual(replayed.G, next.G) || replayed.StateID != next.StateID {
		t.Errorf("Replay diverged: G %+v StateID %d, want %+v %d", replayed.G, replayed.StateID, next.G, next.StateID)
	}

	opponent := other(player)
	if _, err := Apply(game, state, MoveRequest{PlayerID: opponent, Move: "phaseMove", ServerDispatch: true}); !errors.Is(err, ErrInactivePlayer) {
		t.Errorf("server dispatch for an inactive seat: err = %v, want ErrInactivePlayer", err)
	}
}

// TestExclusiveStageReportsStaleStateFirst: a client acting on an old
// StateID is told its view is stale, not that its move is illegal in a
// stage it may no longer be in.
func TestExclusiveStageReportsStaleStateFirst(t *testing.T) {
	game := exclusiveStageGame(true)
	state, player := enterStage(t, game, "enter")
	stale := state.StateID
	state, err := Apply(game, state, MoveRequest{PlayerID: player, Move: "respond", StateID: stale})
	if err != nil {
		t.Fatalf("respond: %v", err)
	}

	next, err := Apply(game, state, MoveRequest{PlayerID: player, Move: "phaseMove", StateID: stale})
	if !errors.Is(err, ErrStaleState) {
		t.Fatalf("stale out-of-stage move: err = %v, want ErrStaleState", err)
	}
	assertUnchanged(t, state, next)
}
