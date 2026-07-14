package core

import "testing"

// TestDropBlocksForClearsStrandedPrompts pins the timeout/forfeit escape
// hatch: an IgnoreBlocks move can drop another player's persisted
// pending block, unblocking the match, while blocks of other players
// survive.
func TestDropBlocksForClearsStrandedPrompts(t *testing.T) {
	type gState struct{}
	game := &Game{
		Name: "dropblocks", MinPlayers: 2, MaxPlayers: 2,
		Setup: func(_ Ctx, _ any) G { return &gState{} },
		Moves: map[string]any{
			"ask": MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
				mc.Queue.Block("pick", mc.PlayerID, nil)
				return mc.G, nil
			}),
			"forceTimeout": Move{
				AnyPlayer:    true,
				IgnoreBlocks: true,
				Move: MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
					mc.DropBlocksFor(mc.Ctx.CurrentPlayer)
					mc.Events.EndTurn("")
					return mc.G, nil
				}),
			},
			"normal": MoveFn(func(mc *MoveContext, _ ...any) (G, error) { return mc.G, nil }),
		},
		Turn: &TurnConfig{MinMoves: 0, MaxMoves: 10},
	}

	state := NewMatch(game, 2, nil)
	next, err := Apply(game, state, MoveRequest{PlayerID: "0", Move: "ask"})
	if err != nil {
		t.Fatalf("ask: %v", err)
	}
	if len(next.Blocks) != 1 {
		t.Fatalf("expected 1 pending block, got %d", len(next.Blocks))
	}

	// Ordinary move still gated.
	if _, err := Apply(game, next, MoveRequest{PlayerID: "0", Move: "normal"}); err == nil {
		t.Fatal("ordinary move should be blocked while a prompt is pending")
	}

	// The opponent forces the timeout: block dropped, turn rotated.
	after, err := Apply(game, next, MoveRequest{PlayerID: "1", Move: "forceTimeout"})
	if err != nil {
		t.Fatalf("forceTimeout: %v", err)
	}
	if len(after.Blocks) != 0 {
		t.Fatalf("stranded block not dropped: %v", after.Blocks)
	}
	if after.Ctx.CurrentPlayer != "1" {
		t.Fatalf("turn should have rotated to P1, cur=%s", after.Ctx.CurrentPlayer)
	}
	// The pre-move state (rollback source) must be untouched.
	if len(next.Blocks) != 1 {
		t.Fatalf("pre-move state's blocks mutated (rollback aliasing): %v", next.Blocks)
	}
}
