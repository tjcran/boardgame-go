package bots_test

import (
	"context"
	"testing"

	"github.com/tjcran/boardgame-go/bots"
	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/games/tictactoe"
)

func TestSimulateAggregatesAcrossMatches(t *testing.T) {
	g := tictactoe.New()
	g.Enumerate = func(gg core.G, _ core.Ctx, _ string) []core.EnumerateAction {
		st := gg.(*tictactoe.State)
		var out []core.EnumerateAction
		for i, c := range st.Cells {
			if c == "" {
				out = append(out, core.EnumerateAction{Move: "clickCell", Args: []any{i}})
			}
		}
		return out
	}
	bot := &bots.RandomBot{Game: g, Seed: int64(7)}

	result, err := bots.Simulate(context.Background(), bots.SimulateOptions{
		Game:    g,
		Bots:    map[string]bots.Bot{"0": bot, "1": bot},
		Matches: 20,
	})
	if err != nil {
		t.Fatalf("simulate: %v", err)
	}
	if result.Matches != 20 {
		t.Fatalf("expected 20 matches, got %d", result.Matches)
	}
	// Some combination of wins + draws should account for all
	// non-errored matches.
	total := result.Wins["0"] + result.Wins["1"] + result.Draws + result.Errors
	if total != 20 {
		t.Fatalf("expected 20 = wins[0]+wins[1]+draws+errors, got %d (%+v)", total, result)
	}
	if result.AvgMoves <= 0 {
		t.Fatalf("expected positive AvgMoves, got %v", result.AvgMoves)
	}
}

func TestSimulateRespectsContextCancel(t *testing.T) {
	g := tictactoe.New()
	g.Enumerate = func(gg core.G, _ core.Ctx, _ string) []core.EnumerateAction {
		st := gg.(*tictactoe.State)
		var out []core.EnumerateAction
		for i, c := range st.Cells {
			if c == "" {
				out = append(out, core.EnumerateAction{Move: "clickCell", Args: []any{i}})
			}
		}
		return out
	}
	bot := &bots.RandomBot{Game: g, Seed: int64(1)}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancelled
	result, err := bots.Simulate(ctx, bots.SimulateOptions{
		Game: g, Bots: map[string]bots.Bot{"0": bot, "1": bot}, Matches: 1000,
	})
	if err == nil {
		t.Fatal("expected context cancellation to bubble up as error")
	}
	if result.Matches > 5 {
		t.Fatalf("expected near-zero matches after pre-cancel, got %d", result.Matches)
	}
}
