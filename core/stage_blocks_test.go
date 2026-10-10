package core

import (
	"errors"
	"testing"
)

// TestPendingBlocksGateStageMoves: while a block is pending, a move that
// does not answer it is refused with ErrBlocked unless the move the
// player would run is IgnoreBlocks. The gate judges that move from the
// player's own stage table, like the rest of Apply: a move registered
// only in a stage table is gated, and a stage move that shadows an
// IgnoreBlocks top-level name is gated by its own flag.
func TestPendingBlocksGateStageMoves(t *testing.T) {
	game := &Game{
		Name:       "stage-blocks-test",
		MinPlayers: 2,
		MaxPlayers: 2,
		Setup:      func(_ Ctx, _ any) G { return &targetState{} },
		Moves: map[string]any{
			"enterAndAsk": MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
				mc.Events.SetActivePlayers(ActivePlayersConfig{CurrentPlayer: Stage("respond")})
				mc.Queue.Block("confirm", mc.PlayerID, nil)
				return cloneT(mc.G.(*targetState), "enterAndAsk"), nil
			}),
			"escape":   Move{IgnoreBlocks: true, Move: recordMove("escape")},
			"shadowed": Move{IgnoreBlocks: true, Move: recordMove("shadowed")},
		},
		Turn: &TurnConfig{Stages: map[string]*StageConfig{
			"respond": {Moves: map[string]any{
				"respond":    recordMove("respond"),
				"stageEsc":   Move{IgnoreBlocks: true, Move: recordMove("stageEsc")},
				"shadowed":   recordMove("shadowed"),
				"stageReply": recordMove("stageReply"),
			}},
		}},
	}
	cases := []struct {
		move    string
		wantErr error // nil = the move must run
	}{
		{move: "respond", wantErr: ErrBlocked},
		{move: "shadowed", wantErr: ErrBlocked},
		{move: "stageEsc"},
		{move: "escape"},
	}
	for _, tc := range cases {
		t.Run(tc.move, func(t *testing.T) {
			state, player := enterStage(t, game, "enterAndAsk")
			if len(state.Blocks) != 1 {
				t.Fatalf("Blocks = %+v, want one pending block", state.Blocks)
			}
			next, err := Apply(game, state, MoveRequest{PlayerID: player, Move: tc.move})
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("%s: err = %v, want %v", tc.move, err, tc.wantErr)
				}
				assertUnchanged(t, state, next)
				return
			}
			if err != nil {
				t.Fatalf("%s: %v", tc.move, err)
			}
			if events := next.G.(*targetState).Events; events[len(events)-1] != tc.move {
				t.Fatalf("%s did not run: events = %v", tc.move, events)
			}
		})
	}

	// The block's answer still goes through from the stage table.
	state, player := enterStage(t, game, "enterAndAsk")
	if _, err := Apply(game, state, MoveRequest{PlayerID: player, Move: "stageReply", ResumeTag: "confirm"}); err != nil {
		t.Fatalf("stage answer to the pending block: %v", err)
	}
}
