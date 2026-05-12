package core

import "testing"

// discardGame: classic BGIO stages example. The current player plays a card;
// every *other* player must discard before the turn ends.
//
// Validates: SetActivePlayers(Others), stage-local Moves, EndStage,
// turn auto-end when active set drains, current player can still move
// during active stages (in BGIO this is allowed when CurrentPlayer is also
// in activePlayers; otherwise it isn't).
func discardGame(events *[]string) *Game {
	type state struct{ Discards int }
	return &Game{
		Name:       "discard",
		MinPlayers: 3,
		MaxPlayers: 3,
		Setup:      func(_ Ctx, _ any) G { return &state{} },
		Moves: map[string]any{
			"play": MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
				*events = append(*events, "play:"+mc.PlayerID)
				mc.Events.SetActivePlayers(ActivePlayersConfig{
					Others:   Stage("discard"),
					MinMoves: 1, MaxMoves: 1,
				})
				return mc.G, nil
			}),
		},
		Turn: &TurnConfig{
			Stages: map[string]*StageConfig{
				"discard": {
					Moves: map[string]any{
						"Discard": MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
							*events = append(*events, "discard:"+mc.PlayerID)
							s := mc.G.(*state)
							return &state{Discards: s.Discards + 1}, nil
						}),
					},
				},
			},
		},
	}
}

func TestSetActivePlayersOthersGatesMoves(t *testing.T) {
	var events []string
	g := discardGame(&events)
	s := NewMatch(g, 0, nil)
	s, err := Apply(g, s, MoveRequest{PlayerID: "0", Move: "play"})
	if err != nil {
		t.Fatalf("play: %v", err)
	}
	// Player 0 should not be active any more (others-only).
	if _, ok := s.Ctx.ActivePlayers["0"]; ok {
		t.Fatalf("player 0 should not be active after Others SetActivePlayers")
	}
	// Players 1 and 2 should be in stage "discard".
	if s.Ctx.ActivePlayers["1"] != "discard" || s.Ctx.ActivePlayers["2"] != "discard" {
		t.Fatalf("expected p1,p2 in stage 'discard', got %v", s.Ctx.ActivePlayers)
	}
	// Player 0 cannot move while not active.
	_, err = Apply(g, s, MoveRequest{PlayerID: "0", Move: "play"})
	if err == nil {
		t.Fatal("expected error for inactive player 0")
	}
}

func TestActivePlayersDrainAndReturn(t *testing.T) {
	var events []string
	g := discardGame(&events)
	s := NewMatch(g, 0, nil)
	s, _ = Apply(g, s, MoveRequest{PlayerID: "0", Move: "play"})
	// Both others discard.
	s, _ = Apply(g, s, MoveRequest{PlayerID: "1", Move: "Discard"})
	s, _ = Apply(g, s, MoveRequest{PlayerID: "2", Move: "Discard"})

	if s.Ctx.ActivePlayers != nil {
		t.Fatalf("expected ActivePlayers to drain, got %v", s.Ctx.ActivePlayers)
	}
	// After draining, ctx returns to current player owning the turn.
	if s.Ctx.CurrentPlayer != "0" {
		t.Fatalf("current player should still be 0, got %s", s.Ctx.CurrentPlayer)
	}
}

func TestEndStageRotatesToStageNext(t *testing.T) {
	g := &Game{
		Name:       "cycle",
		MinPlayers: 2,
		MaxPlayers: 2,
		Setup:      func(_ Ctx, _ any) G { return struct{}{} },
		Moves: map[string]any{
			"enter": MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
				mc.Events.SetStage("A")
				return mc.G, nil
			}),
		},
		Turn: &TurnConfig{
			Stages: map[string]*StageConfig{
				"A": {
					Next: "B",
					Moves: map[string]any{
						"adv": MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
							mc.Events.EndStage()
							return mc.G, nil
						}),
					},
				},
				"B": {
					Moves: map[string]any{
						"adv": noopMove,
					},
				},
			},
		},
	}
	s := NewMatch(g, 0, nil)
	s, _ = Apply(g, s, MoveRequest{PlayerID: "0", Move: "enter"})
	if s.Ctx.ActivePlayers["0"] != "A" {
		t.Fatalf("expected p0 in A, got %v", s.Ctx.ActivePlayers)
	}
	s, _ = Apply(g, s, MoveRequest{PlayerID: "0", Move: "adv"})
	if s.Ctx.ActivePlayers["0"] != "B" {
		t.Fatalf("expected p0 in B after EndStage, got %v", s.Ctx.ActivePlayers)
	}
}
