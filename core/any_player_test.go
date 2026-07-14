package core

import "testing"

// TestAnyPlayerMoveBypassesTurnOwnership pins the AnyPlayer contract:
// a non-current player can dispatch an AnyPlayer move (concede /
// opponent-forced timeout), while ordinary moves still reject with
// ErrWrongPlayer.
func TestAnyPlayerMoveBypassesTurnOwnership(t *testing.T) {
	type gState struct {
		ForcedBy string `json:"forcedBy"`
	}
	game := &Game{
		Name: "anyplayer", MinPlayers: 2, MaxPlayers: 2,
		Setup: func(_ Ctx, _ any) G { return &gState{} },
		Moves: map[string]any{
			"normal": MoveFn(func(mc *MoveContext, _ ...any) (G, error) { return mc.G, nil }),
			"force": Move{
				AnyPlayer: true,
				Move: MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
					s := mc.G.(*gState)
					s.ForcedBy = mc.PlayerID
					mc.Events.EndTurn("")
					return s, nil
				}),
			},
		},
		Turn: &TurnConfig{MinMoves: 0, MaxMoves: 10},
	}
	state := NewMatch(game, 2, nil)
	if state.Ctx.CurrentPlayer != "0" {
		t.Fatalf("expected P0 first, got %s", state.Ctx.CurrentPlayer)
	}

	// Ordinary move from the wrong seat still rejects.
	if _, err := Apply(game, state, MoveRequest{PlayerID: "1", Move: "normal"}); err == nil {
		t.Fatal("normal move from non-current player must reject")
	}

	// AnyPlayer move from the wrong seat applies and can end the turn.
	next, err := Apply(game, state, MoveRequest{PlayerID: "1", Move: "force"})
	if err != nil {
		t.Fatalf("AnyPlayer move rejected: %v", err)
	}
	if got := next.G.(*gState).ForcedBy; got != "1" {
		t.Fatalf("move body saw playerID %q, want 1", got)
	}
	if next.Ctx.CurrentPlayer != "1" {
		t.Fatalf("turn should have rotated, cur=%s", next.Ctx.CurrentPlayer)
	}
}
