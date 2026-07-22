package random_test

import (
	"encoding/json"
	"testing"

	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/plugins/random"
)

// hookRNGGame ends its turn from the only move it defines, so Turn.OnEnd
// runs as an engine hook rather than as move code. The hook records what
// it saw on its MoveContext and consumes one draw from the PRNG.
func hookRNGGame(sawRandom *bool, rolls *[]float64) *core.Game {
	return &core.Game{
		Name:    "hook-rng",
		Plugins: []core.Plugin{random.New(nil)},
		Setup:   func(_ core.Ctx, _ any) core.G { return map[string]any{} },
		Turn: &core.TurnConfig{
			OnEnd: func(mc *core.MoveContext) core.G {
				if mc.Random == nil {
					return mc.G
				}
				*sawRandom = true
				*rolls = append(*rolls, mc.Random.Number())
				return mc.G
			},
		},
		Moves: map[string]any{
			"end": core.MoveFn(func(mc *core.MoveContext, _ ...any) (core.G, error) {
				mc.Events.EndTurn()
				return mc.G, nil
			}),
		},
	}
}

// A turn hook is game code exactly like a move is; if the engine builds its
// MoveContext without the plugin table, mc.Random is nil and any game that
// randomises at end of turn silently becomes deterministic.
func TestTurnHookGetsRandom(t *testing.T) {
	var saw bool
	var rolls []float64
	game := hookRNGGame(&saw, &rolls)

	st := core.NewMatchSeeded(game, 2, nil, 12345)
	if _, err := core.Apply(game, st, core.MoveRequest{PlayerID: "0", Move: "end"}); err != nil {
		t.Fatal(err)
	}
	if !saw {
		t.Fatal("Turn.OnEnd saw mc.Random == nil; hook MoveContexts must carry the plugin table")
	}
}

// The plugin API and the persisted plugin data share a state pointer, so a
// draw taken inside a hook must advance the match's PRNG for good — if it
// doesn't, every turn restarts from the same state and the "random" hook
// repeats itself forever.
func TestHookRandomMutationPersistsIntoMatchState(t *testing.T) {
	var saw bool
	var rolls []float64
	game := hookRNGGame(&saw, &rolls)

	st := core.NewMatchSeeded(game, 2, nil, 12345)
	before := prngState(t, st)

	for turn := range 3 {
		next, err := core.Apply(game, st, core.MoveRequest{
			PlayerID: st.Ctx.CurrentPlayer, Move: "end",
		})
		if err != nil {
			t.Fatalf("turn %d: %v", turn, err)
		}
		st = next
	}

	if !saw {
		t.Fatal("Turn.OnEnd saw mc.Random == nil")
	}
	if got := prngState(t, st); got == before {
		t.Fatalf("PRNG state unchanged (%d) after three hook draws; hook mutations are not reaching State.Plugins", got)
	}
	if len(rolls) != 3 {
		t.Fatalf("got %d hook rolls, want 3", len(rolls))
	}
	if rolls[0] == rolls[1] || rolls[1] == rolls[2] {
		t.Fatalf("hook rolls repeat (%v); each turn restarted from the same PRNG state", rolls)
	}
}

// prngState reads the Random plugin's persisted state out of the match by
// its wire shape, so the probe doesn't depend on the plugin's internals.
func prngState(t *testing.T, st core.State) uint64 {
	t.Helper()
	data, ok := st.Plugins[random.PluginName]
	if !ok {
		t.Fatal("no random plugin data on the match state")
	}
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	var shape struct {
		S uint64 `json:"s"`
	}
	if err := json.Unmarshal(raw, &shape); err != nil {
		t.Fatal(err)
	}
	return shape.S
}
