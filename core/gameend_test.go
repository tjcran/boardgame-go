package core

import "testing"

func TestEndGameEventWritesGameoverAndRunsOnEnd(t *testing.T) {
	onEndCalled := false
	g := &Game{
		Name:       "explicit-end",
		MinPlayers: 2,
		MaxPlayers: 2,
		Setup:      func(_ Ctx, _ any) G { return struct{}{} },
		Moves: map[string]any{
			"surrender": MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
				mc.Events.EndGame(map[string]any{"winner": "1"})
				return mc.G, nil
			}),
		},
		OnEnd: func(mc *MoveContext) G {
			onEndCalled = true
			return mc.G
		},
		Turn: &TurnConfig{MinMoves: 1, MaxMoves: 1},
	}
	s := NewMatch(g, 0, nil)
	s, err := Apply(g, s, MoveRequest{PlayerID: "0", Move: "surrender"})
	if err != nil {
		t.Fatalf("surrender: %v", err)
	}
	if !onEndCalled {
		t.Fatal("expected OnEnd hook to fire")
	}
	if m, _ := s.Ctx.Gameover.(map[string]any); m["winner"] != "1" {
		t.Fatalf("expected ctx.Gameover.winner=1, got %v", s.Ctx.Gameover)
	}
}

func TestSetupDataReachesSetup(t *testing.T) {
	type state struct{ Greeting string }
	g := &Game{
		Name:  "setup-data",
		Setup: func(_ Ctx, sd any) G { return &state{Greeting: sd.(string)} },
		Moves: map[string]any{
			"noop": noopMove,
		},
		Turn: &TurnConfig{MinMoves: 1, MaxMoves: 1},
	}
	s := NewMatch(g, 0, "hello")
	if s.G.(*state).Greeting != "hello" {
		t.Fatalf("expected greeting from setupData, got %+v", s.G)
	}
}

func TestValidateSetupDataRejectsBadInput(t *testing.T) {
	g := &Game{
		Name: "validate",
		Setup: func(_ Ctx, _ any) G { return struct{}{} },
		ValidateSetupData: func(sd any, _ int) string {
			if sd == nil {
				return "setupData required"
			}
			return ""
		},
	}
	if msg := g.ValidateSetupData(nil, 2); msg == "" {
		t.Fatal("expected validation error for nil setupData")
	}
}
