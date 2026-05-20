package typedgame_test

import (
	"testing"

	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/typedgame"
)

// counterState exercises the generic shell: an int counter incremented by
// the only move. No runtime assertions in the move body — the compiler
// already knows mc.G is *counterState.
type counterState struct{ Score int }

func TestTypedGameBuildsAndRuns(t *testing.T) {
	g := typedgame.Game[*counterState, any]{
		Name:       "counter",
		MinPlayers: 1,
		MaxPlayers: 1,
		Setup:      func(_ core.Ctx, _ any) *counterState { return &counterState{} },
		Moves: typedgame.Moves[*counterState]{
			"inc": typedgame.MoveFn[*counterState](func(mc *typedgame.Context[*counterState], _ ...any) (*counterState, error) {
				// mc.G is *counterState — no .(*X) anywhere.
				return &counterState{Score: mc.G.Score + 1}, nil
			}),
		},
		EndIf: func(mc *typedgame.Context[*counterState]) any {
			if mc.G.Score >= 3 {
				return map[string]any{"winner": mc.Ctx.CurrentPlayer}
			}
			return nil
		},
		Turn: &typedgame.TurnConfig[*counterState]{MinMoves: 1, MaxMoves: 1},
	}.Build()

	s := core.NewMatch(g, 0, nil)
	for i := 0; i < 3; i++ {
		next, err := core.Apply(g, s, core.MoveRequest{PlayerID: "0", Move: "inc"})
		if err != nil {
			t.Fatalf("inc %d: %v", i, err)
		}
		s = next
	}
	if s.Ctx.Gameover == nil {
		t.Fatalf("expected gameover after 3 incs, got %+v", s.Ctx)
	}
	if m, _ := s.Ctx.Gameover.(map[string]any); m["winner"] != "0" {
		t.Fatalf("expected winner=0, got %v", s.Ctx.Gameover)
	}
}

func TestTypedGameHooksReceiveTypedG(t *testing.T) {
	var sawOnBegin int
	g := typedgame.Game[*counterState, any]{
		Name:       "counter",
		MinPlayers: 1,
		MaxPlayers: 1,
		Setup:      func(_ core.Ctx, _ any) *counterState { return &counterState{Score: 7} },
		Moves: typedgame.Moves[*counterState]{
			"x": typedgame.MoveFn[*counterState](func(mc *typedgame.Context[*counterState], _ ...any) (*counterState, error) {
				return mc.G, nil
			}),
		},
		Turn: &typedgame.TurnConfig[*counterState]{
			OnBegin: func(mc *typedgame.Context[*counterState]) *counterState {
				sawOnBegin = mc.G.Score
				return mc.G
			},
			MinMoves: 1, MaxMoves: 1,
		},
	}.Build()

	core.NewMatch(g, 0, nil)
	if sawOnBegin != 7 {
		t.Fatalf("expected OnBegin to see typed G.Score=7, got %d", sawOnBegin)
	}
}

func TestTypedMoveLongFormCarriesFlags(t *testing.T) {
	g := typedgame.Game[*counterState, any]{
		Name:       "counter",
		MinPlayers: 1,
		MaxPlayers: 1,
		Setup:      func(_ core.Ctx, _ any) *counterState { return &counterState{} },
		Moves: typedgame.Moves[*counterState]{
			"inc": typedgame.Move[*counterState]{
				Move: func(mc *typedgame.Context[*counterState], _ ...any) (*counterState, error) {
					return &counterState{Score: mc.G.Score + 1}, nil
				},
				Undoable: false,
				NoLimit:  true,
			},
		},
		Turn: &typedgame.TurnConfig[*counterState]{MinMoves: 1, MaxMoves: 1},
	}.Build()

	s := core.NewMatch(g, 0, nil)
	// NoLimit means this move shouldn't auto-end the turn even though
	// MaxMoves=1; current player should still be "0" after the call.
	s, err := core.Apply(g, s, core.MoveRequest{PlayerID: "0", Move: "inc"})
	if err != nil {
		t.Fatalf("inc: %v", err)
	}
	if s.Ctx.CurrentPlayer != "0" {
		t.Fatalf("expected NoLimit to prevent auto-end-turn, got current=%s", s.Ctx.CurrentPlayer)
	}
}

// MySetup is a non-trivial typed setup-data payload — proves the SD type
// parameter actually constrains what Setup receives.
type MySetup struct {
	StartingScore int
	DeckSeed      int64
}

func TestTypedSetupDataReachesSetupCallback(t *testing.T) {
	type State struct {
		Score int
		Seed  int64
	}
	g := typedgame.Game[*State, *MySetup]{
		Name: "typed-setup-test", MinPlayers: 2, MaxPlayers: 2,
		Setup: func(_ core.Ctx, sd *MySetup) *State {
			if sd == nil {
				return &State{Score: -1, Seed: -1}
			}
			return &State{Score: sd.StartingScore, Seed: sd.DeckSeed}
		},
		Turn: &typedgame.TurnConfig[*State]{MinMoves: 1, MaxMoves: 1},
	}

	cg := g.Build()
	setup := &MySetup{StartingScore: 42, DeckSeed: 1234567}
	st := core.NewMatch(cg, 0, setup)
	s, ok := st.G.(*State)
	if !ok {
		t.Fatalf("state should be *State, got %T", st.G)
	}
	if s.Score != 42 {
		t.Errorf("Score = %d, want 42 (setupData should arrive typed)", s.Score)
	}
	if s.Seed != 1234567 {
		t.Errorf("Seed = %d, want 1234567", s.Seed)
	}
}

func TestTypedSetupDataNilFallsThroughAsZero(t *testing.T) {
	type State struct{ Got bool }
	g := typedgame.Game[*State, *MySetup]{
		Name: "typed-setup-nil", MinPlayers: 1, MaxPlayers: 1,
		Setup: func(_ core.Ctx, sd *MySetup) *State {
			return &State{Got: sd == nil}
		},
		Turn: &typedgame.TurnConfig[*State]{MinMoves: 1, MaxMoves: 1},
	}
	cg := g.Build()
	st := core.NewMatch(cg, 0, nil)
	s := st.G.(*State)
	if !s.Got {
		t.Errorf("nil setup data should arrive as nil-typed (zero value of *MySetup)")
	}
}

func TestTypedSetupDataWrongTypePanics(t *testing.T) {
	type State struct{}
	g := typedgame.Game[*State, *MySetup]{
		Name: "typed-setup-mismatch", MinPlayers: 1, MaxPlayers: 1,
		Setup: func(_ core.Ctx, _ *MySetup) *State {
			return &State{}
		},
		Turn: &typedgame.TurnConfig[*State]{MinMoves: 1, MaxMoves: 1},
	}
	cg := g.Build()
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on wrong setupData type, got no panic")
		}
	}()
	_ = core.NewMatch(cg, 0, "not-a-MySetup")
}

func TestTypedValidateSetupDataReceivesTypedValue(t *testing.T) {
	type State struct{}
	var observed *MySetup
	g := typedgame.Game[*State, *MySetup]{
		Name: "typed-validate", MinPlayers: 2, MaxPlayers: 2,
		Setup: func(_ core.Ctx, _ *MySetup) *State { return &State{} },
		ValidateSetupData: func(sd *MySetup, numPlayers int) string {
			observed = sd
			if sd == nil {
				return "missing setup"
			}
			if sd.StartingScore < 0 {
				return "negative score"
			}
			return ""
		},
		Turn: &typedgame.TurnConfig[*State]{MinMoves: 1, MaxMoves: 1},
	}
	cg := g.Build()
	if cg.ValidateSetupData == nil {
		t.Fatal("ValidateSetupData should not be nil after Build")
	}

	if msg := cg.ValidateSetupData(&MySetup{StartingScore: 5}, 2); msg != "" {
		t.Errorf("valid setup should pass validation, got %q", msg)
	}
	if observed == nil || observed.StartingScore != 5 {
		t.Errorf("ValidateSetupData callback should have received typed *MySetup, got %v", observed)
	}

	if msg := cg.ValidateSetupData(&MySetup{StartingScore: -1}, 2); msg != "negative score" {
		t.Errorf("invalid setup should fail validation, got %q", msg)
	}
}
