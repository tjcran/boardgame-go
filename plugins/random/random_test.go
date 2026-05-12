package random_test

import (
	"reflect"
	"testing"

	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/plugins/random"
)

// diceState is the per-match G payload for the test fixture. Module-level
// so reflection-friendly assertions across helper closures work.
type diceState struct{ Last int }

// diceGame is the parity test fixture: each move rolls a d6 and stores
// the result. Different seeds must produce different rolls; the same seed
// must produce the same rolls.
func diceGame(seed any) *core.Game {
	return &core.Game{
		Name:       "dice",
		MinPlayers: 1,
		MaxPlayers: 1,
		Setup:      func(_ core.Ctx, _ any) core.G { return &diceState{} },
		Plugins:    []core.Plugin{random.New(seed)},
		Moves: map[string]any{
			"roll": core.MoveFn(func(mc *core.MoveContext, _ ...any) (core.G, error) {
				roll := mc.Random.D6().(int)
				return &diceState{Last: roll}, nil
			}),
		},
		Turn: &core.TurnConfig{MinMoves: 1, MaxMoves: 1},
	}
}

func TestRandomDeterministicGivenSeed(t *testing.T) {
	runOnce := func() []int {
		g := diceGame(42)
		s := core.NewMatch(g, 0, nil)
		var rolls []int
		for i := 0; i < 5; i++ {
			next, err := core.Apply(g, s, core.MoveRequest{PlayerID: "0", Move: "roll"})
			if err != nil {
				t.Fatalf("roll %d: %v", i, err)
			}
			rolls = append(rolls, next.G.(*diceState).Last)
			s = next
		}
		return rolls
	}
	a := runOnce()
	b := runOnce()
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("expected deterministic sequence, got %v vs %v", a, b)
	}
}

func TestRandomDifferentSeedsDifferentRolls(t *testing.T) {
	first := func(seed any) int {
		g := diceGame(seed)
		s := core.NewMatch(g, 0, nil)
		next, err := core.Apply(g, s, core.MoveRequest{PlayerID: "0", Move: "roll"})
		if err != nil {
			t.Fatalf("roll: %v", err)
		}
		return next.G.(*diceState).Last
	}
	// Try a handful of seeds; at least one pair must differ. Roll values
	// are 1..6 so it's possible (but vanishingly unlikely) all match.
	seeds := []any{1, 2, "alpha", "beta", 12345}
	rolls := make(map[int]bool)
	for _, s := range seeds {
		rolls[first(s)] = true
	}
	if len(rolls) < 2 {
		t.Fatalf("expected variety from different seeds, got %v", rolls)
	}
}

func TestRandomPRNGStateHiddenInPlayerView(t *testing.T) {
	g := diceGame("secret-seed")
	s := core.NewMatch(g, 0, nil)
	// Plug data should exist post-setup.
	if _, ok := s.Plugins[random.PluginName]; !ok {
		t.Fatal("random plugin data missing")
	}
	view := core.PlayerView(g, s, "0")
	if v, ok := view.Plugins[random.PluginName]; ok && v != nil {
		t.Fatalf("PRNG state leaked into PlayerView: %v", v)
	}
}

func TestShuffleProducesPermutation(t *testing.T) {
	g := diceGame(7)
	s := core.NewMatch(g, 0, nil)
	apis := s.Plugins[random.PluginName]
	r := (&random.Plugin{}).API(apis, s.G, s.Ctx, "", g).(*core.Random)

	deck := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	shuffled := core.Shuffle(r, deck)
	if len(shuffled) != len(deck) {
		t.Fatalf("shuffled length mismatch: %d vs %d", len(shuffled), len(deck))
	}
	seen := map[int]int{}
	for _, v := range shuffled {
		seen[v]++
	}
	for _, v := range deck {
		if seen[v] != 1 {
			t.Fatalf("element %d appears %d times after shuffle", v, seen[v])
		}
	}
}
