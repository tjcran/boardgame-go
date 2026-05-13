package bots_test

import (
	"context"
	"testing"
	"time"

	"github.com/tjcran/boardgame-go/bots"
	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/games/tictactoe"
	"github.com/tjcran/boardgame-go/match"
	"github.com/tjcran/boardgame-go/storage"
)

// enumerateTTT lists every empty cell as a clickCell action.
func enumerateTTT(g core.G, _ core.Ctx, _ string) []bots.Action {
	st := g.(*tictactoe.State)
	var out []bots.Action
	for i, c := range st.Cells {
		if c == "" {
			out = append(out, bots.Action{Move: "clickCell", Args: []any{i}})
		}
	}
	return out
}

func TestRandomBotPicksALegalMove(t *testing.T) {
	g := tictactoe.New()
	s := core.NewMatch(g, 0, nil)
	bot := &bots.RandomBot{Enumerate: enumerateTTT, Seed: int64(42)}
	action, err := bot.Play(context.Background(), s, "0")
	if err != nil {
		t.Fatalf("play: %v", err)
	}
	// Apply it to verify it's legal.
	if _, err := core.Apply(g, s, core.MoveRequest{
		PlayerID: "0", Move: action.Move, Args: action.Args,
	}); err != nil {
		t.Fatalf("RandomBot picked an illegal action %+v: %v", action, err)
	}
}

func TestMCTSBotPrefersWinningCompletion(t *testing.T) {
	// Build a position where X has two in a row and a winning third cell.
	g := tictactoe.New()
	s := core.NewMatch(g, 0, nil)
	// Manually engineer the position: X at 0, X at 1, O at 6.
	// Easiest: play moves.
	steps := [][2]any{
		{"0", 0}, {"1", 6}, {"0", 1}, {"1", 7},
	}
	for _, step := range steps {
		next, err := core.Apply(g, s, core.MoveRequest{
			PlayerID: step[0].(string), Move: "clickCell", Args: []any{step[1]},
		})
		if err != nil {
			t.Fatalf("setup: %v", err)
		}
		s = next
	}
	// Now it's X's turn. The winning move is cell 2.
	bot := &bots.MCTSBot{
		Game:       g,
		Enumerate:  enumerateTTT,
		Iterations: 200,
		Seed:       int64(7),
	}
	action, err := bot.Play(context.Background(), s, "0")
	if err != nil {
		t.Fatalf("play: %v", err)
	}
	if idx, _ := action.Args[0].(int); idx != 2 {
		// Print warning rather than fail: MCTS with 200 iters might still
		// occasionally miss on a degenerate seed. But on seed=7 we expect
		// it to find the win.
		t.Logf("MCTS picked cell %v (expected 2 for top-row win)", action.Args[0])
		if idx != 2 {
			t.Fail()
		}
	}
}

// TestBGIO906_MCTSEarlyStopHalts verifies the EarlyStop callback can
// terminate MCTS before Iterations is exhausted. Addresses BGIO #906.
func TestBGIO906_MCTSEarlyStopHalts(t *testing.T) {
	g := tictactoe.New()
	s := core.NewMatch(g, 0, nil)

	called := 0
	bot := &bots.MCTSBot{
		Game:           g,
		Enumerate:      enumerateTTT,
		Iterations:     1000,
		Seed:           int64(42),
		EarlyStopAfter: 4,
		EarlyStop: func(_ []bots.Action, _ []float64, visits []int) bool {
			called++
			total := 0
			for _, v := range visits {
				total += v
			}
			return total >= 20 // halt after 20 iterations
		},
	}
	if _, err := bot.Play(context.Background(), s, "0"); err != nil {
		t.Fatalf("play: %v", err)
	}
	if called == 0 {
		t.Fatal("EarlyStop callback never invoked")
	}
	// Past the warmup floor (16) and the halt threshold (20), we expect
	// called to be in [5, 200] range — not exhaustive proof, just a
	// floor on "the callback short-circuited the loop."
	if called > 200 {
		t.Fatalf("EarlyStop didn't halt the loop (called %d times for 1000 iters)", called)
	}
}

func TestAutoPlayerActsOnItsTurn(t *testing.T) {
	m := match.NewManager(storage.NewMemory())
	m.MustRegister(tictactoe.New())
	id, _ := m.Create("tic-tac-toe", match.CreateOptions{})
	alice, _ := m.Join(id, "alice", match.JoinOptions{})
	bob, _ := m.Join(id, "bob", match.JoinOptions{})

	bot := &bots.RandomBot{Enumerate: enumerateTTT, Seed: int64(123)}
	auto := &bots.AutoPlayer{
		Bot:         bot,
		Manager:     m,
		MatchID:     id,
		PlayerID:    bob.PlayerID,
		Credentials: bob.PlayerCredentials,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() {
		_ = auto.Run(ctx)
		close(done)
	}()

	// alice plays cell 0; the bot (bob) should respond.
	_, err := m.MoveReqCtx(ctx, id, alice.PlayerID, alice.PlayerCredentials, core.MoveRequest{
		Move: "clickCell", Args: []any{0},
	})
	if err != nil {
		t.Fatalf("alice's move: %v", err)
	}

	// Wait briefly for the bot to react.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		mm, _ := m.State(id)
		if mm.State.Ctx.CurrentPlayer == alice.PlayerID && mm.State.Ctx.Turn >= 3 {
			cancel()
			<-done
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("bot did not respond within deadline")
}
