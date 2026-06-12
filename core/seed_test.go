package core

import (
	"encoding/json"
	"testing"
)

func seedTestGame() *Game {
	return &Game{
		Name: "seed-test",
		Setup: func(ctx Ctx, _ any) G {
			// Setup observes the seed so tests can prove it was present
			// before Setup ran (RNG-using games need exactly that).
			return map[string]any{"setup_seed": ctx.Seed}
		},
		Moves: map[string]any{
			"noop": MoveFn(func(mc *MoveContext, _ ...any) (G, error) { return mc.G, nil }),
		},
	}
}

func TestNewMatchSeededSetsCtxSeedBeforeSetup(t *testing.T) {
	st := NewMatchSeeded(seedTestGame(), 2, nil, 42)
	if st.Ctx.Seed != 42 {
		t.Fatalf("Ctx.Seed = %d, want 42", st.Ctx.Seed)
	}
	g := st.G.(map[string]any)
	if g["setup_seed"] != uint64(42) {
		t.Fatalf("Setup saw seed %v, want 42 (seed must be set before Setup)", g["setup_seed"])
	}
}

func TestNewMatchDefaultsToZeroSeed(t *testing.T) {
	st := NewMatch(seedTestGame(), 2, nil)
	if st.Ctx.Seed != 0 {
		t.Fatalf("NewMatch Ctx.Seed = %d, want 0 (legacy behavior)", st.Ctx.Seed)
	}
}

func TestPlayerViewStripsSeed(t *testing.T) {
	game := seedTestGame()
	st := NewMatchSeeded(game, 2, nil, 99)
	for _, viewer := range []string{"0", "1", ""} {
		view := PlayerView(game, st, viewer)
		if view.Ctx.Seed != 0 {
			t.Fatalf("PlayerView(%q) leaked seed %d", viewer, view.Ctx.Seed)
		}
	}
	if st.Ctx.Seed != 99 {
		t.Fatal("PlayerView must not mutate the source state")
	}
}

func TestReplaySeededReproducesSeededMatch(t *testing.T) {
	game := seedTestGame()
	st := NewMatchSeeded(game, 2, nil, 7)
	next, err := Apply(game, st, MoveRequest{PlayerID: "0", Move: "noop"})
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := ReplaySeeded(game, next.Log, 2, nil, 7)
	if err != nil {
		t.Fatal(err)
	}
	origJSON, _ := json.Marshal(next.G)
	replJSON, _ := json.Marshal(replayed.G)
	if string(origJSON) != string(replJSON) || replayed.Ctx.Seed != 7 {
		t.Fatalf("seeded replay mismatch: orig=%s repl=%s seed=%d", origJSON, replJSON, replayed.Ctx.Seed)
	}
}

// Compat guard: a Ctx that never sets Seed must marshal to the exact
// pre-change wire bytes, and the seed must round-trip storage JSON.
func TestCtxSeedWireShape(t *testing.T) {
	raw, err := json.Marshal(Ctx{NumPlayers: 2, CurrentPlayer: "0", PlayOrder: []string{"0", "1"}, Turn: 1})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"numPlayers":2,"currentPlayer":"0","playOrder":["0","1"],"playOrderPos":0,"turn":1,"activePlayers":{}}`
	if string(raw) != want {
		t.Fatalf("zero-seed wire shape changed:\n got %s\nwant %s", raw, want)
	}

	seeded, _ := json.Marshal(Ctx{NumPlayers: 2, Seed: 5})
	var back Ctx
	if err := json.Unmarshal(seeded, &back); err != nil {
		t.Fatal(err)
	}
	if back.Seed != 5 {
		t.Fatalf("seed did not round-trip: %d", back.Seed)
	}
}
