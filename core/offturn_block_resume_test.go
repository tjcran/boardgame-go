package core

import (
	"errors"
	"testing"
)

// offTurnBlockGame builds a two-player game where the CURRENT player's
// move raises a target prompt addressed to the OTHER player — the shape
// of any out-of-turn prompt (a reaction window on an opponent's turn,
// "your opponent picks a card from their hand", a simultaneous response).
//
//   - "askOpponent" pushes one RequestTarget owned by the non-current
//     player.
//   - "askOpponentTwice" chains: the first answer raises a second prompt
//     for the same player, so no fixed move budget an opener could set up
//     front would cover it.
//   - "pickTarget" is the resume move, validated against
//     mc.ResumingBlock.Target.
//   - "noop" is an unrelated move, used to show non-resume moves stay
//     gated while a block is pending.
func offTurnBlockGame() *Game {
	ask := func(mc *MoveContext, kind string) {
		mc.Queue.RequestTarget(other(mc.Ctx.CurrentPlayer), TargetRequest{
			Kind: kind, Candidates: []any{10, 20}, Min: 1, Max: 1, Source: "reaction",
		})
	}
	return &Game{
		Name:       "off-turn-block-test",
		MinPlayers: 2,
		MaxPlayers: 2,
		Setup:      func(_ Ctx, _ any) G { return &targetState{} },
		Moves: map[string]any{
			"askOpponent": MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
				ask(mc, "pick")
				return cloneT(mc.G.(*targetState), "askOpponent"), nil
			}),
			"askOpponentTwice": MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
				ask(mc, "pick")
				return cloneT(mc.G.(*targetState), "askOpponentTwice"), nil
			}),
			"pickTarget": MoveFn(func(mc *MoveContext, args ...any) (G, error) {
				g := cloneT(mc.G.(*targetState), "pickTarget")
				if mc.ResumingBlock == nil || mc.ResumingBlock.Target == nil {
					return g, errors.New("pickTarget expected a ResumingBlock with Target")
				}
				if err := ValidateSelection(*mc.ResumingBlock.Target, args); err != nil {
					return mc.G, err
				}
				g.Selection = append(g.Selection, args...)
				// A chained prompt: answering "pick" raises "again" for the
				// same off-turn player.
				if mc.ResumingBlock.Target.Kind == "pick" && len(g.Events) > 0 &&
					g.Events[0] == "askOpponentTwice" {
					mc.Queue.RequestTarget(mc.PlayerID, TargetRequest{
						Kind: "again", Candidates: []any{30, 40}, Min: 1, Max: 1, Source: "chain",
					})
				}
				return g, nil
			}),
			"noop": MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
				return cloneT(mc.G.(*targetState), "noop"), nil
			}),
		},
	}
}

func other(playerID string) string {
	if playerID == "0" {
		return "1"
	}
	return "0"
}

// TestOffTurnBlockOwnerMayResume is the regression test for the off-turn
// prompt deadlock: a block names the player who must answer it, and
// findBlock matches on PlayerID, so resolving a ResumeTag already proves
// the caller is that player. Before the fix, authorizedStage ran right
// afterwards and rejected them with ErrWrongPlayer for not holding the
// turn — while every other seat was held off by ErrBlocked on the same
// unanswered block, leaving nobody able to move.
func TestOffTurnBlockOwnerMayResume(t *testing.T) {
	game := offTurnBlockGame()
	state := NewMatch(game, 2, nil)
	turnOwner := state.Ctx.CurrentPlayer
	responder := other(turnOwner)

	state, err := Apply(game, state, MoveRequest{PlayerID: turnOwner, Move: "askOpponent"})
	if err != nil {
		t.Fatalf("askOpponent: %v", err)
	}
	if len(state.Blocks) != 1 {
		t.Fatalf("blocks = %d, want 1", len(state.Blocks))
	}
	if got := state.Blocks[0].PlayerID; got != responder {
		t.Fatalf("block owner = %q, want %q", got, responder)
	}

	state, err = Apply(game, state, MoveRequest{
		PlayerID: responder, Move: "pickTarget", Args: []any{10}, ResumeTag: "pick",
	})
	if err != nil {
		t.Fatalf("off-turn block owner could not answer their own prompt: %v", err)
	}
	if len(state.Blocks) != 0 {
		t.Errorf("blocks = %d, want 0 (prompt answered)", len(state.Blocks))
	}
	if got := state.Ctx.CurrentPlayer; got != turnOwner {
		t.Errorf("current player = %q, want %q (answering a prompt is not a turn)", got, turnOwner)
	}
}

// TestOffTurnBlockGatesOtherSeats is the other half of the deadlock: the
// turn owner is correctly held off by ErrBlocked while the opponent's
// prompt is open. Pinning it keeps the two halves from diverging — the
// gate is only survivable because the addressee can answer.
func TestOffTurnBlockGatesOtherSeats(t *testing.T) {
	game := offTurnBlockGame()
	state := NewMatch(game, 2, nil)
	turnOwner := state.Ctx.CurrentPlayer

	state, err := Apply(game, state, MoveRequest{PlayerID: turnOwner, Move: "askOpponent"})
	if err != nil {
		t.Fatalf("askOpponent: %v", err)
	}

	if _, err := Apply(game, state, MoveRequest{PlayerID: turnOwner, Move: "noop"}); !errors.Is(err, ErrBlocked) {
		t.Errorf("turn owner during an opponent's prompt: err = %v, want ErrBlocked", err)
	}
}

// TestOffTurnResumeStillRejectsNonOwners: authorization comes from the
// block, so a seat with no matching block gains nothing. findBlock keys
// on (tag, playerID), so a wrong-seat resume fails on the tag lookup
// rather than silently resuming someone else's prompt.
func TestOffTurnResumeStillRejectsNonOwners(t *testing.T) {
	game := offTurnBlockGame()
	state := NewMatch(game, 2, nil)
	turnOwner := state.Ctx.CurrentPlayer

	state, err := Apply(game, state, MoveRequest{PlayerID: turnOwner, Move: "askOpponent"})
	if err != nil {
		t.Fatalf("askOpponent: %v", err)
	}

	_, err = Apply(game, state, MoveRequest{
		PlayerID: turnOwner, Move: "pickTarget", Args: []any{10}, ResumeTag: "pick",
	})
	if !errors.Is(err, ErrUnknownResumeTag) {
		t.Errorf("resume by a seat holding no such block: err = %v, want ErrUnknownResumeTag", err)
	}
}

// TestOffTurnChainedPromptsStayAnswerable: an answer that raises the next
// prompt must stay answerable too. This is what a fixed per-opener move
// budget cannot express — the opener would have to know up front how many
// moves the whole chain takes.
func TestOffTurnChainedPromptsStayAnswerable(t *testing.T) {
	game := offTurnBlockGame()
	state := NewMatch(game, 2, nil)
	turnOwner := state.Ctx.CurrentPlayer
	responder := other(turnOwner)

	state, err := Apply(game, state, MoveRequest{PlayerID: turnOwner, Move: "askOpponentTwice"})
	if err != nil {
		t.Fatalf("askOpponentTwice: %v", err)
	}

	state, err = Apply(game, state, MoveRequest{
		PlayerID: responder, Move: "pickTarget", Args: []any{10}, ResumeTag: "pick",
	})
	if err != nil {
		t.Fatalf("first answer: %v", err)
	}
	if len(state.Blocks) != 1 || state.Blocks[0].Tag != "again" {
		t.Fatalf("blocks = %+v, want one chained \"again\" block", state.Blocks)
	}

	state, err = Apply(game, state, MoveRequest{
		PlayerID: responder, Move: "pickTarget", Args: []any{30}, ResumeTag: "again",
	})
	if err != nil {
		t.Fatalf("chained answer: %v", err)
	}
	if len(state.Blocks) != 0 {
		t.Errorf("blocks = %d, want 0 (chain fully answered)", len(state.Blocks))
	}
}

// TestOffTurnResumeKeepsStageScope: when the block owner IS listed in
// ActivePlayers, the resume must still resolve from their stage's move
// table, not fall back to the global scope.
func TestOffTurnResumeKeepsStageScope(t *testing.T) {
	stageOnly := MoveFn(func(mc *MoveContext, args ...any) (G, error) {
		g := cloneT(mc.G.(*targetState), "stageResume")
		g.SawResume = mc.ResumingBlock != nil
		return g, nil
	})
	game := &Game{
		Name:       "off-turn-stage-scope-test",
		MinPlayers: 2,
		MaxPlayers: 2,
		Setup:      func(_ Ctx, _ any) G { return &targetState{} },
		Turn: &TurnConfig{
			Stages: map[string]*StageConfig{
				"responding": {Moves: map[string]any{"resume": stageOnly}},
			},
		},
		Moves: map[string]any{
			"askOpponent": MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
				opp := other(mc.Ctx.CurrentPlayer)
				mc.Queue.RequestTarget(opp, TargetRequest{
					Kind: "pick", Candidates: []any{10}, Min: 1, Max: 1,
				})
				mc.Events.SetActivePlayers(ActivePlayersConfig{
					Value: map[string]string{opp: "responding"}, Revert: true,
				})
				return cloneT(mc.G.(*targetState), "askOpponent"), nil
			}),
		},
	}

	state := NewMatch(game, 2, nil)
	turnOwner := state.Ctx.CurrentPlayer
	responder := other(turnOwner)

	state, err := Apply(game, state, MoveRequest{PlayerID: turnOwner, Move: "askOpponent"})
	if err != nil {
		t.Fatalf("askOpponent: %v", err)
	}

	state, err = Apply(game, state, MoveRequest{
		PlayerID: responder, Move: "resume", Args: []any{10}, ResumeTag: "pick",
	})
	if err != nil {
		t.Fatalf("stage-scoped resume: %v", err)
	}
	if !state.G.(*targetState).SawResume {
		t.Error("stage-scoped move did not run as a resume")
	}
}
