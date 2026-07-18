package core

import (
	"errors"
	"testing"
)

// multiBlockGame builds a game where a single move raises TWO target
// prompts for the same player in one apply — the shape a cascade
// produces when one action summons several entities whose triggered
// abilities each ask for a manual choice.
//
//   - "askForTwo" pushes two RequestTargets with the SAME Kind
//     ("pick"), so both blocks carry the same tag — the reducer must
//     resolve them positionally (first match wins).
//   - "askForTwoKinds" pushes one "pick" and one "burn" request, so
//     the tags differ and either can be resumed first.
//   - "pickTarget" is the resume move: it validates the selection
//     against mc.ResumingBlock.Target and records it.
//   - "noop" is an unrelated move used to prove non-resume moves stay
//     gated while any block is pending.
func multiBlockGame() *Game {
	return &Game{
		Name:       "multi-block-test",
		MinPlayers: 1,
		MaxPlayers: 1,
		Setup:      func(_ Ctx, _ any) G { return &targetState{} },
		Moves: map[string]any{
			"askForTwo": MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
				mc.Queue.RequestTarget(mc.PlayerID, TargetRequest{
					Kind: "pick", Candidates: []any{10, 20}, Min: 1, Max: 1, Source: "summon-A",
				})
				mc.Queue.RequestTarget(mc.PlayerID, TargetRequest{
					Kind: "pick", Candidates: []any{30, 40}, Min: 1, Max: 1, Source: "summon-B",
				})
				return cloneT(mc.G.(*targetState), "askForTwo"), nil
			}),
			"askForTwoKinds": MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
				mc.Queue.RequestTarget(mc.PlayerID, TargetRequest{
					Kind: "pick", Candidates: []any{10, 20}, Min: 1, Max: 1, Source: "summon-A",
				})
				mc.Queue.RequestTarget(mc.PlayerID, TargetRequest{
					Kind: "burn", Candidates: []any{30, 40}, Min: 1, Max: 1, Source: "summon-B",
				})
				return cloneT(mc.G.(*targetState), "askForTwoKinds"), nil
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
				return g, nil
			}),
			"noop": MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
				return cloneT(mc.G.(*targetState), "noop"), nil
			}),
		},
	}
}

// TestResumeProceedsWithRemainingBlocks is the regression test for the
// multi-prompt soft-lock: with two blocks pending for the same seat, a
// move that resumes one of them must APPLY, leaving the other pending.
// Before the fix the reducer consumed the resumed block, then hit the
// ErrBlocked gate on the remaining one and rolled the whole move back —
// making any state with 2+ simultaneous blocks permanently stuck for
// every move except IgnoreBlocks escapes (timeout/concede).
func TestResumeProceedsWithRemainingBlocks(t *testing.T) {
	game := multiBlockGame()
	state := NewMatch(game, 1, nil)

	state, err := Apply(game, state, MoveRequest{PlayerID: "0", Move: "askForTwo"})
	if err != nil {
		t.Fatalf("askForTwo: %v", err)
	}
	if len(state.Blocks) != 2 {
		t.Fatalf("expected 2 pending blocks, got %d", len(state.Blocks))
	}

	// Resume the first prompt. Positional: with equal tags, findBlock
	// consumes Blocks[0] (candidates 10/20).
	state, err = Apply(game, state, MoveRequest{
		PlayerID: "0", Move: "pickTarget", ResumeTag: "pick", Args: []any{10},
	})
	if err != nil {
		t.Fatalf("resume with a remaining block must apply, got: %v", err)
	}
	if len(state.Blocks) != 1 {
		t.Fatalf("expected 1 block to remain, got %d", len(state.Blocks))
	}
	if state.Blocks[0].Target.Source != "summon-B" {
		t.Fatalf("expected summon-B's block to remain, got %+v", state.Blocks[0])
	}

	// The remaining block still gates non-resume moves.
	if _, err := Apply(game, state, MoveRequest{PlayerID: "0", Move: "noop"}); !errors.Is(err, ErrBlocked) {
		t.Fatalf("non-resume move with a pending block: want ErrBlocked, got %v", err)
	}

	// Resume the second prompt; the match fully unblocks.
	state, err = Apply(game, state, MoveRequest{
		PlayerID: "0", Move: "pickTarget", ResumeTag: "pick", Args: []any{40},
	})
	if err != nil {
		t.Fatalf("second resume: %v", err)
	}
	if len(state.Blocks) != 0 {
		t.Fatalf("expected no blocks after both resumes, got %d", len(state.Blocks))
	}
	g := state.G.(*targetState)
	if len(g.Selection) != 2 || g.Selection[0] != 10 || g.Selection[1] != 40 {
		t.Fatalf("expected selections [10 40], got %v", g.Selection)
	}

	if _, err := Apply(game, state, MoveRequest{PlayerID: "0", Move: "noop"}); err != nil {
		t.Fatalf("noop after all blocks resolved: %v", err)
	}
}

// TestResumeDistinctTagsAnyOrder covers the two-kind case: blocks with
// different tags can be resumed in either order, and a selection is
// validated against the block the tag actually names.
func TestResumeDistinctTagsAnyOrder(t *testing.T) {
	game := multiBlockGame()
	state := NewMatch(game, 1, nil)

	state, err := Apply(game, state, MoveRequest{PlayerID: "0", Move: "askForTwoKinds"})
	if err != nil {
		t.Fatalf("askForTwoKinds: %v", err)
	}

	// Resume the SECOND block ("burn") first.
	state, err = Apply(game, state, MoveRequest{
		PlayerID: "0", Move: "pickTarget", ResumeTag: "burn", Args: []any{30},
	})
	if err != nil {
		t.Fatalf("resume burn with pick still pending: %v", err)
	}
	if len(state.Blocks) != 1 || state.Blocks[0].Tag != "pick" {
		t.Fatalf("expected the pick block to remain, got %+v", state.Blocks)
	}

	// A selection outside the resumed block's candidates still fails
	// inside the move, and the failed apply must not consume the block.
	if _, err := Apply(game, state, MoveRequest{
		PlayerID: "0", Move: "pickTarget", ResumeTag: "pick", Args: []any{999},
	}); err == nil {
		t.Fatal("expected invalid selection to fail")
	}
	if len(state.Blocks) != 1 {
		t.Fatalf("failed resume must roll the block back, got %d blocks", len(state.Blocks))
	}

	state, err = Apply(game, state, MoveRequest{
		PlayerID: "0", Move: "pickTarget", ResumeTag: "pick", Args: []any{20},
	})
	if err != nil {
		t.Fatalf("resume pick: %v", err)
	}
	if len(state.Blocks) != 0 {
		t.Fatalf("expected no blocks, got %d", len(state.Blocks))
	}
}
