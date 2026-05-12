package core

import "testing"

// twoPhaseGame: phase "draw" lets players play "draw" to increment G.deck;
// when deck reaches 3, phase "draw" ends and "play" begins. In "play",
// players call "play" to decrement.
//
// Validates: Start, OnBegin/OnEnd hooks, EndIf, Next, phase-local Moves,
// phase-local Turn config.
func twoPhaseGame(events *[]string) *Game {
	type state struct{ Deck int }
	rec := func(name string) HookFn {
		return func(mc *MoveContext) G { *events = append(*events, name); return mc.G }
	}
	return &Game{
		Name:       "two-phase",
		MinPlayers: 2,
		MaxPlayers: 2,
		Setup:      func(_ Ctx, _ any) G { return &state{} },
		Turn:       &TurnConfig{MinMoves: 1, MaxMoves: 1},
		Phases: map[string]*PhaseConfig{
			"draw": {
				Start:   true,
				OnBegin: rec("draw.onBegin"),
				OnEnd:   rec("draw.onEnd"),
				Next:    "play",
				EndIf: func(mc *MoveContext) (bool, string) {
					return mc.G.(*state).Deck >= 3, ""
				},
				Moves: map[string]any{
					"draw": MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
						s := mc.G.(*state)
						return &state{Deck: s.Deck + 1}, nil
					}),
				},
			},
			"play": {
				OnBegin: rec("play.onBegin"),
				OnEnd:   rec("play.onEnd"),
				Moves: map[string]any{
					"play": MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
						s := mc.G.(*state)
						return &state{Deck: s.Deck - 1}, nil
					}),
				},
			},
		},
	}
}

func TestStartingPhaseRuns(t *testing.T) {
	var events []string
	g := twoPhaseGame(&events)
	s := NewMatch(g, 0, nil)
	if s.Ctx.Phase != "draw" {
		t.Fatalf("expected starting phase draw, got %q", s.Ctx.Phase)
	}
	if len(events) == 0 || events[0] != "draw.onBegin" {
		t.Fatalf("expected draw.onBegin to have fired, got %v", events)
	}
}

func TestPhaseLocalMovesOverrideGlobal(t *testing.T) {
	var events []string
	g := twoPhaseGame(&events)
	s := NewMatch(g, 0, nil)
	// "play" is not in scope while we're in "draw".
	_, err := Apply(g, s, MoveRequest{PlayerID: "0", Move: "play"})
	if err == nil {
		t.Fatal("expected unknown-move error for out-of-phase move")
	}
}

func TestPhaseEndIfRotatesPhase(t *testing.T) {
	var events []string
	g := twoPhaseGame(&events)
	s := NewMatch(g, 0, nil)

	// Three "draw" moves: deck reaches 3, EndIf fires, phase rotates to play.
	for i := 0; i < 3; i++ {
		next, err := Apply(g, s, MoveRequest{PlayerID: s.Ctx.CurrentPlayer, Move: "draw"})
		if err != nil {
			t.Fatalf("draw %d: %v", i, err)
		}
		s = next
	}
	if s.Ctx.Phase != "play" {
		t.Fatalf("expected rotation to phase 'play', got %q", s.Ctx.Phase)
	}
	if !contains(events, "draw.onEnd") || !contains(events, "play.onBegin") {
		t.Fatalf("expected draw.onEnd and play.onBegin in events: %v", events)
	}
}

func TestSetPhaseEventTransitions(t *testing.T) {
	var events []string
	g := twoPhaseGame(&events)
	// Inject a move into draw phase that calls SetPhase("play").
	g.Phases["draw"].Moves["jump"] = MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
		mc.Events.SetPhase("play")
		return mc.G, nil
	})
	s := NewMatch(g, 0, nil)
	next, err := Apply(g, s, MoveRequest{PlayerID: "0", Move: "jump"})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if next.Ctx.Phase != "play" {
		t.Fatalf("expected SetPhase to rotate to 'play', got %q", next.Ctx.Phase)
	}
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
