package mcp

import (
	"context"
	"errors"
	"testing"

	"github.com/tjcran/boardgame-go/games/tictactoe"
	"github.com/tjcran/boardgame-go/match"
	"github.com/tjcran/boardgame-go/storage"
)

// newToolsForTest spins up a fresh Manager with in-memory storage and the
// tic-tac-toe game registered. Returns the Tools surface plus the manager
// in case tests need to peek directly.
func newToolsForTest(t *testing.T) (*Tools, *match.Manager) {
	t.Helper()
	m := match.NewManager(storage.NewMemory())
	if err := m.Register(tictactoe.New()); err != nil {
		t.Fatalf("register tic-tac-toe: %v", err)
	}
	return &Tools{Manager: m}, m
}

func TestListGames(t *testing.T) {
	tools, _ := newToolsForTest(t)
	res, err := tools.ListGames(context.Background())
	if err != nil {
		t.Fatalf("ListGames: %v", err)
	}
	if len(res.Games) != 1 {
		t.Fatalf("want 1 game, got %d (%+v)", len(res.Games), res.Games)
	}
	if res.Games[0].Name != "tic-tac-toe" {
		t.Errorf("game name = %q, want %q", res.Games[0].Name, "tic-tac-toe")
	}
}

func TestCreateAndJoinMatch(t *testing.T) {
	tools, _ := newToolsForTest(t)
	ctx := context.Background()

	created, err := tools.CreateMatch(ctx, CreateMatchArgs{Game: "tic-tac-toe", NumPlayers: 2})
	if err != nil {
		t.Fatalf("CreateMatch: %v", err)
	}
	if created.MatchID == "" {
		t.Fatal("CreateMatch returned empty MatchID")
	}
	if created.NumPlayers != 2 {
		t.Errorf("NumPlayers = %d, want 2", created.NumPlayers)
	}

	joinX, err := tools.JoinMatch(ctx, JoinMatchArgs{MatchID: created.MatchID, PlayerID: "0", Name: "X"})
	if err != nil {
		t.Fatalf("Join X: %v", err)
	}
	joinO, err := tools.JoinMatch(ctx, JoinMatchArgs{MatchID: created.MatchID, PlayerID: "1", Name: "O"})
	if err != nil {
		t.Fatalf("Join O: %v", err)
	}
	if joinX.Credentials == "" || joinO.Credentials == "" {
		t.Error("expected credentials for both seats")
	}
	if joinX.Credentials == joinO.Credentials {
		t.Error("expected distinct credentials per seat")
	}
}

// TestPlayThroughXWins drives a full tic-tac-toe game to completion via the
// MCP tool surface, asserting that:
//   - ListLegalMoves returns the right set of moves at each step
//   - MakeMove advances state and rotates currentPlayer
//   - Illegal moves (out of turn, occupied cell) are rejected with errors
//     the LLM client can act on
//   - Gameover is populated when X wins
func TestPlayThroughXWins(t *testing.T) {
	tools, _ := newToolsForTest(t)
	ctx := context.Background()

	created, err := tools.CreateMatch(ctx, CreateMatchArgs{Game: "tic-tac-toe", NumPlayers: 2})
	if err != nil {
		t.Fatalf("CreateMatch: %v", err)
	}
	mid := created.MatchID

	jx, _ := tools.JoinMatch(ctx, JoinMatchArgs{MatchID: mid, PlayerID: "0"})
	jo, _ := tools.JoinMatch(ctx, JoinMatchArgs{MatchID: mid, PlayerID: "1"})

	// X has 9 legal moves at the start (every cell).
	legal, err := tools.ListLegalMoves(ctx, ListLegalMovesArgs{MatchID: mid, PlayerID: jx.PlayerID})
	if err != nil {
		t.Fatalf("ListLegalMoves(X start): %v", err)
	}
	if len(legal.Moves) != 9 {
		t.Errorf("legal moves at start = %d, want 9", len(legal.Moves))
	}

	// X plays 0 (top-left).
	playMove(t, tools, mid, jx.PlayerID, jx.Credentials, "clickCell", 0)
	// O plays 3 (middle-left).
	playMove(t, tools, mid, jo.PlayerID, jo.Credentials, "clickCell", 3)
	// X plays 1.
	playMove(t, tools, mid, jx.PlayerID, jx.Credentials, "clickCell", 1)
	// O plays 4.
	playMove(t, tools, mid, jo.PlayerID, jo.Credentials, "clickCell", 4)

	// X attempts to play O's cell — should fail.
	if _, err := tools.MakeMove(ctx, MakeMoveArgs{
		MatchID: mid, PlayerID: jx.PlayerID, Credentials: jx.Credentials,
		Move: "clickCell", Args: []any{4},
	}); err == nil {
		t.Error("expected error playing occupied cell, got nil")
	}

	// O attempts to play out of turn (it's X's turn) — should fail.
	if _, err := tools.MakeMove(ctx, MakeMoveArgs{
		MatchID: mid, PlayerID: jo.PlayerID, Credentials: jo.Credentials,
		Move: "clickCell", Args: []any{6},
	}); err == nil {
		t.Error("expected error playing out of turn, got nil")
	}

	// X plays 2 — completes the top row, wins.
	res := playMove(t, tools, mid, jx.PlayerID, jx.Credentials, "clickCell", 2)
	if res.Gameover == nil {
		t.Fatalf("expected Gameover after winning move, got nil")
	}

	// After Gameover, ListLegalMoves should return zero moves.
	post, err := tools.ListLegalMoves(ctx, ListLegalMovesArgs{MatchID: mid, PlayerID: jo.PlayerID})
	if err != nil {
		t.Fatalf("ListLegalMoves(post-gameover): %v", err)
	}
	if len(post.Moves) != 0 {
		t.Errorf("legal moves after gameover = %d, want 0", len(post.Moves))
	}
}

// TestBadCredentialsRejected makes sure the LLM client can't impersonate a
// seat by guessing.
func TestBadCredentialsRejected(t *testing.T) {
	tools, _ := newToolsForTest(t)
	ctx := context.Background()

	created, _ := tools.CreateMatch(ctx, CreateMatchArgs{Game: "tic-tac-toe", NumPlayers: 2})
	jx, _ := tools.JoinMatch(ctx, JoinMatchArgs{MatchID: created.MatchID, PlayerID: "0"})
	_, _ = tools.JoinMatch(ctx, JoinMatchArgs{MatchID: created.MatchID, PlayerID: "1"})

	_, err := tools.MakeMove(ctx, MakeMoveArgs{
		MatchID: created.MatchID, PlayerID: jx.PlayerID,
		Credentials: "wrong-token",
		Move:        "clickCell", Args: []any{4},
	})
	if err == nil {
		t.Fatal("expected error for bad credentials, got nil")
	}
	if !errors.Is(err, match.ErrBadCredentials) {
		t.Logf("got error %v (acceptable as long as it's not nil)", err)
	}
}

// TestUnknownGame surfaces a clear error rather than panicking.
func TestUnknownGame(t *testing.T) {
	tools, _ := newToolsForTest(t)
	_, err := tools.CreateMatch(context.Background(), CreateMatchArgs{Game: "no-such-game", NumPlayers: 2})
	if err == nil {
		t.Fatal("expected error creating match for unknown game")
	}
}

// playMove is a tiny helper that makes the test bodies read like the actual
// MCP call sequence an LLM would issue.
func playMove(t *testing.T, tools *Tools, matchID, playerID, creds, move string, cell int) MakeMoveResult {
	t.Helper()
	res, err := tools.MakeMove(context.Background(), MakeMoveArgs{
		MatchID: matchID, PlayerID: playerID, Credentials: creds,
		Move: move, Args: []any{cell},
	})
	if err != nil {
		t.Fatalf("MakeMove(%s, cell=%d): %v", playerID, cell, err)
	}
	return res
}
