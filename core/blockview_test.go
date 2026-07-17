package core

import (
	"encoding/json"
	"testing"
)

// blockViewTestGame returns a minimal game with a "pick" move that resolves
// a block queued by an "ask" move — enough to exercise BlockView and the
// resume path without any transport.
func blockViewTestGame(view BlockViewFn) *Game {
	return &Game{
		Name: "blockview-test",
		Moves: map[string]any{
			"ask": MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
				mc.Queue.Block("pick", mc.PlayerID, map[string]any{
					"candidates": []string{"card-7", "card-9"},
				})
				return mc.G, nil
			}),
			"pick": MoveFn(func(mc *MoveContext, args ...any) (G, error) {
				return map[string]any{"picked": args[0]}, nil
			}),
		},
		Turn:      &TurnConfig{MinMoves: 0, MaxMoves: 10},
		BlockView: view,
	}
}

func TestPlayerViewNilBlockViewLeavesBlocksUnchanged(t *testing.T) {
	game := blockViewTestGame(nil)
	state := State{
		Blocks: []BlockSpec{
			{Tag: "pick", PlayerID: "0", Data: map[string]any{"candidates": []string{"a", "b"}}},
		},
	}

	before, err := json.Marshal(state.Blocks)
	if err != nil {
		t.Fatalf("marshal source: %v", err)
	}

	for _, viewer := range []string{"0", "1", ""} {
		view := PlayerView(game, state, viewer)
		got, err := json.Marshal(view.Blocks)
		if err != nil {
			t.Fatalf("marshal view for %q: %v", viewer, err)
		}
		if string(got) != string(before) {
			t.Fatalf("nil BlockView must leave wire bytes unchanged for viewer %q: got %s, want %s",
				viewer, got, before)
		}
	}
}

func TestPlayerViewAppliesBlockViewPerViewer(t *testing.T) {
	game := blockViewTestGame(HideBlockPayload)
	state := State{
		Blocks: []BlockSpec{
			{
				Tag:      "pick",
				PlayerID: "0",
				Data:     map[string]any{"candidates": []string{"card-7", "card-9"}},
				Target:   &TargetRequest{Kind: "pick", Candidates: []any{"card-7", "card-9"}},
			},
		},
	}

	// The addressed seat sees the full payload.
	own := PlayerView(game, state, "0")
	if own.Blocks[0].Data == nil || own.Blocks[0].Target == nil {
		t.Fatalf("addressed seat should see its own block payload; got %+v", own.Blocks[0])
	}

	// A different seat sees the shell only.
	other := PlayerView(game, state, "1")
	if got := other.Blocks[0]; got.Tag != "pick" || got.PlayerID != "0" {
		t.Fatalf("ownership shell should survive redaction; got %+v", got)
	}
	if other.Blocks[0].Data != nil || other.Blocks[0].Target != nil {
		t.Fatalf("block payload leaked to a different seat: %+v", other.Blocks[0])
	}

	// Spectators (playerID "") match no seat either.
	spec := PlayerView(game, state, "")
	if spec.Blocks[0].Data != nil || spec.Blocks[0].Target != nil {
		t.Fatalf("block payload leaked to spectator: %+v", spec.Blocks[0])
	}

	// Two viewers, same authoritative state, different wire payloads —
	// this is the whole point of the hook.
	ownJSON, _ := json.Marshal(own.Blocks)
	otherJSON, _ := json.Marshal(other.Blocks)
	if string(ownJSON) == string(otherJSON) {
		t.Fatalf("expected different payloads per viewer, both got %s", ownJSON)
	}
}

func TestPlayerViewBlockViewDoesNotMutateSource(t *testing.T) {
	game := blockViewTestGame(HideBlockPayload)
	state := State{
		Blocks: []BlockSpec{
			{Tag: "pick", PlayerID: "0", Data: map[string]any{"x": 1}},
		},
	}

	_ = PlayerView(game, state, "1")

	if state.Blocks[0].Data == nil {
		t.Fatalf("PlayerView must not mutate the authoritative state's block payload: %+v", state.Blocks[0])
	}
}

func TestHideBlockPayload(t *testing.T) {
	block := BlockSpec{
		Tag: "pick", PlayerID: "0", Order: 3,
		Data:   map[string]any{"secret": true},
		Target: &TargetRequest{Kind: "pick"},
	}

	own := HideBlockPayload(block, "0")
	if own.Data == nil || own.Target == nil {
		t.Fatalf("owning viewer should see the payload unchanged: %+v", own)
	}

	other := HideBlockPayload(block, "1")
	if other.Data != nil || other.Target != nil {
		t.Fatalf("non-owning viewer should have payload stripped: %+v", other)
	}
	if other.Tag != "pick" || other.PlayerID != "0" || other.Order != 3 {
		t.Fatalf("shell fields must survive redaction: %+v", other)
	}

	spectator := HideBlockPayload(block, "")
	if spectator.Data != nil || spectator.Target != nil {
		t.Fatalf("spectator should have payload stripped: %+v", spectator)
	}

	// Source block is untouched — HideBlockPayload takes block by value.
	if block.Data == nil || block.Target == nil {
		t.Fatalf("HideBlockPayload mutated its input: %+v", block)
	}
}

// TestBlockViewRedactionDoesNotBreakResume pins the invariant that a
// viewer-facing BlockView redaction can never affect move validation:
// Apply always runs against the authoritative State (never a PlayerView
// output), so the resume move must succeed identically whether or not
// some other viewer was ever shown a redacted copy of the block.
func TestBlockViewRedactionDoesNotBreakResume(t *testing.T) {
	game := blockViewTestGame(HideBlockPayload)
	state, err := Apply(game, NewMatch(game, 2, nil), MoveRequest{PlayerID: "0", Move: "ask"})
	if err != nil {
		t.Fatalf("ask: %v", err)
	}
	if len(state.Blocks) != 1 {
		t.Fatalf("expected one pending block, got %d", len(state.Blocks))
	}

	// Produce redacted views for a non-owning seat and a spectator, as a
	// broadcast to other subscribers would. This must not touch the
	// authoritative state the reducer resumes against.
	_ = PlayerView(game, state, "1")
	_ = PlayerView(game, state, "")

	// The addressed seat still resumes normally.
	resumed, err := Apply(game, state, MoveRequest{
		PlayerID: "0", Move: "pick", Args: []any{"card-7"}, ResumeTag: "pick",
	})
	if err != nil {
		t.Fatalf("resume after redaction: %v", err)
	}
	if len(resumed.Blocks) != 0 {
		t.Fatalf("resume should clear the block: %+v", resumed.Blocks)
	}
	got, ok := resumed.G.(map[string]any)
	if !ok || got["picked"] != "card-7" {
		t.Fatalf("resume move did not apply: G=%+v", resumed.G)
	}
}
