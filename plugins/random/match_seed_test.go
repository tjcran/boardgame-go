package random_test

import (
	"encoding/json"
	"testing"

	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/plugins/random"
)

// seedGame draws once per move and appends the result to G, so the recorded
// sequence is a direct read-out of the match's PRNG stream.
func seedGame(plugin core.Plugin) *core.Game {
	return &core.Game{
		Name:    "match-seed",
		Plugins: []core.Plugin{plugin},
		Setup:   func(_ core.Ctx, _ any) core.G { return map[string]any{"draws": []any{}} },
		Moves: map[string]any{
			"draw": core.MoveFn(func(mc *core.MoveContext, _ ...any) (core.G, error) {
				g := mc.G.(map[string]any)
				g["draws"] = append(g["draws"].([]any), mc.Random.Number())
				return g, nil
			}),
		},
	}
}

// drawSequence plays n "draw" moves and returns the resulting stream.
func drawSequence(t *testing.T, game *core.Game, st core.State, n int) string {
	t.Helper()
	for i := range n {
		next, err := core.Apply(game, st, core.MoveRequest{
			PlayerID: st.Ctx.CurrentPlayer, Move: "draw",
		})
		if err != nil {
			t.Fatalf("draw %d: %v", i, err)
		}
		st = next
	}
	raw, err := json.Marshal(st.G)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// The per-match secret seed exists so two matches of the same game don't
// share a PRNG stream. If the plugin ignores Ctx.Seed, the Nth draw is the
// same value in every match the process ever runs.
func TestDifferentMatchSeedsDiverge(t *testing.T) {
	game := seedGame(random.New(nil))

	a := drawSequence(t, game, core.NewMatchSeeded(game, 2, nil, 1), 5)
	b := drawSequence(t, game, core.NewMatchSeeded(game, 2, nil, 999999), 5)

	if a == b {
		t.Fatalf("matches seeded 1 and 999999 produced an identical stream: %s", a)
	}
}

// The flip side of the same property: the same seed must reproduce exactly.
// This is what the splitmix64 choice exists to protect.
func TestSameMatchSeedReproduces(t *testing.T) {
	game := seedGame(random.New(nil))

	a := drawSequence(t, game, core.NewMatchSeeded(game, 2, nil, 4242), 5)
	b := drawSequence(t, game, core.NewMatchSeeded(game, 2, nil, 4242), 5)

	if a != b {
		t.Fatalf("same seed diverged:\n a=%s\n b=%s", a, b)
	}
}

// ReplaySeeded reconstructs from NewMatchSeeded, so an RNG-dependent match
// must replay byte-identically given the seed the match was created with.
func TestSeededReplayReproducesRNGStream(t *testing.T) {
	game := seedGame(random.New(nil))
	const seed = 8675309

	st := core.NewMatchSeeded(game, 2, nil, seed)
	for range 4 {
		next, err := core.Apply(game, st, core.MoveRequest{
			PlayerID: st.Ctx.CurrentPlayer, Move: "draw",
		})
		if err != nil {
			t.Fatal(err)
		}
		st = next
	}

	replayed, err := core.ReplaySeeded(game, st.Log, 2, nil, seed)
	if err != nil {
		t.Fatal(err)
	}
	orig, _ := json.Marshal(st.G)
	repl, _ := json.Marshal(replayed.G)
	if string(orig) != string(repl) {
		t.Fatalf("seeded replay diverged:\n orig=%s\n repl=%s", orig, repl)
	}
}

// An explicit random.New(seed) is a deliberate determinism request (tests,
// reproducible benchmarks) and must keep winning over the per-match seed.
func TestExplicitPluginSeedOverridesMatchSeed(t *testing.T) {
	game := seedGame(random.New(uint64(77)))

	a := drawSequence(t, game, core.NewMatchSeeded(game, 2, nil, 1), 5)
	b := drawSequence(t, game, core.NewMatchSeeded(game, 2, nil, 999999), 5)

	if a != b {
		t.Fatalf("explicit plugin seed did not pin the stream:\n a=%s\n b=%s", a, b)
	}
}

// Game.Seed documents itself as the override for the engine's per-match
// seed, so it too outranks Ctx.Seed.
func TestGameSeedOverridesMatchSeed(t *testing.T) {
	game := seedGame(random.New(nil))
	game.Seed = "fixed-stream"

	a := drawSequence(t, game, core.NewMatchSeeded(game, 2, nil, 1), 5)
	b := drawSequence(t, game, core.NewMatchSeeded(game, 2, nil, 999999), 5)

	if a != b {
		t.Fatalf("Game.Seed did not pin the stream:\n a=%s\n b=%s", a, b)
	}
}
