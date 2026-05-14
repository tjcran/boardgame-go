package mcp

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tjcran/boardgame-go/games/tictactoe"
	"github.com/tjcran/boardgame-go/match"
	"github.com/tjcran/boardgame-go/storage"
)

// newOwnedToolsForTest spins up Tools with ownership scoping enabled,
// for multi-tenant tests.
func newOwnedToolsForTest(t *testing.T) *Tools {
	t.Helper()
	mgr := match.NewManager(storage.NewMemory())
	if err := mgr.Register(tictactoe.New()); err != nil {
		t.Fatalf("register tic-tac-toe: %v", err)
	}
	return &Tools{
		Manager:   mgr,
		Ownership: NewMemoryOwnership(),
	}
}

func TestMemoryOwnership_ClaimAndOwns(t *testing.T) {
	store := NewMemoryOwnership()
	ctx := context.Background()

	if err := store.Claim(ctx, "alice", "match-1"); err != nil {
		t.Fatalf("Claim: %v", err)
	}
	owns, err := store.Owns(ctx, "alice", "match-1")
	if err != nil || !owns {
		t.Errorf("alice should own match-1: owns=%v err=%v", owns, err)
	}
	owns, err = store.Owns(ctx, "bob", "match-1")
	if err != nil || owns {
		t.Errorf("bob should not own match-1: owns=%v err=%v", owns, err)
	}
}

func TestMemoryOwnership_DoubleClaimByOtherUserFails(t *testing.T) {
	store := NewMemoryOwnership()
	ctx := context.Background()
	if err := store.Claim(ctx, "alice", "match-1"); err != nil {
		t.Fatalf("first claim: %v", err)
	}
	if err := store.Claim(ctx, "bob", "match-1"); err == nil {
		t.Error("expected error when a second user claims the same match")
	}
}

func TestMemoryOwnership_IdempotentReclaim(t *testing.T) {
	store := NewMemoryOwnership()
	ctx := context.Background()
	_ = store.Claim(ctx, "alice", "match-1")
	if err := store.Claim(ctx, "alice", "match-1"); err != nil {
		t.Errorf("reclaim by same user should be idempotent, got %v", err)
	}
	matches, _ := store.MatchesFor(ctx, "alice")
	if len(matches) != 1 {
		t.Errorf("expected 1 match for alice, got %d (%v)", len(matches), matches)
	}
}

func TestMemoryOwnership_MatchesFor(t *testing.T) {
	store := NewMemoryOwnership()
	ctx := context.Background()
	_ = store.Claim(ctx, "alice", "m1")
	_ = store.Claim(ctx, "alice", "m2")
	_ = store.Claim(ctx, "bob", "m3")

	aliceMatches, _ := store.MatchesFor(ctx, "alice")
	if len(aliceMatches) != 2 {
		t.Errorf("alice should have 2 matches, got %v", aliceMatches)
	}
	bobMatches, _ := store.MatchesFor(ctx, "bob")
	if len(bobMatches) != 1 || bobMatches[0] != "m3" {
		t.Errorf("bob matches = %v", bobMatches)
	}
}

func TestOwnedTools_CreateRequiresAuth(t *testing.T) {
	tools := newOwnedToolsForTest(t)
	// No userID on context — must fail.
	_, err := tools.CreateMatch(context.Background(), CreateMatchArgs{Game: "tic-tac-toe", NumPlayers: 2})
	if err == nil || !strings.Contains(err.Error(), "not authenticated") {
		t.Errorf("expected not-authenticated error, got %v", err)
	}
}

func TestOwnedTools_CrossUserAccessRejected(t *testing.T) {
	tools := newOwnedToolsForTest(t)
	aliceCtx := WithUserID(context.Background(), "alice")
	bobCtx := WithUserID(context.Background(), "bob")

	created, err := tools.CreateMatch(aliceCtx, CreateMatchArgs{Game: "tic-tac-toe", NumPlayers: 2})
	if err != nil {
		t.Fatalf("alice create: %v", err)
	}

	// Bob tries to join Alice's match — must fail with ErrNotOwned.
	if _, err := tools.JoinMatch(bobCtx, JoinMatchArgs{MatchID: created.MatchID, PlayerID: "0"}); err == nil {
		t.Error("expected ErrNotOwned on cross-user join")
	} else if !errors.Is(err, ErrNotOwned) {
		t.Errorf("expected ErrNotOwned, got %v", err)
	}

	// Bob can't read state either.
	if _, err := tools.GetState(bobCtx, GetStateArgs{MatchID: created.MatchID}); err == nil {
		t.Error("expected error on cross-user get_state")
	}

	// Bob can't enumerate legal moves.
	if _, err := tools.ListLegalMoves(bobCtx, ListLegalMovesArgs{MatchID: created.MatchID, PlayerID: "0"}); err == nil {
		t.Error("expected error on cross-user list_legal_moves")
	}

	// Bob can't make a move.
	if _, err := tools.MakeMove(bobCtx, MakeMoveArgs{
		MatchID: created.MatchID, PlayerID: "0", Credentials: "anything",
		Move: "clickCell", Args: []any{0},
	}); err == nil {
		t.Error("expected error on cross-user make_move")
	}

	// Alice can still see and act on her own match.
	if _, err := tools.GetState(aliceCtx, GetStateArgs{MatchID: created.MatchID}); err != nil {
		t.Errorf("alice should still be able to read her match: %v", err)
	}
}

func TestOwnedTools_FullFlowAsSingleUser(t *testing.T) {
	tools := newOwnedToolsForTest(t)
	ctx := WithUserID(context.Background(), "alice")

	created, err := tools.CreateMatch(ctx, CreateMatchArgs{Game: "tic-tac-toe", NumPlayers: 2})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	jx, err := tools.JoinMatch(ctx, JoinMatchArgs{MatchID: created.MatchID, PlayerID: "0"})
	if err != nil {
		t.Fatalf("join X: %v", err)
	}
	jo, err := tools.JoinMatch(ctx, JoinMatchArgs{MatchID: created.MatchID, PlayerID: "1"})
	if err != nil {
		t.Fatalf("join O: %v", err)
	}
	// One legal move just to verify the path stays unblocked end-to-end.
	if _, err := tools.MakeMove(ctx, MakeMoveArgs{
		MatchID: created.MatchID, PlayerID: jx.PlayerID, Credentials: jx.Credentials,
		Move: "clickCell", Args: []any{4},
	}); err != nil {
		t.Errorf("make_move: %v", err)
	}
	_ = jo
}
