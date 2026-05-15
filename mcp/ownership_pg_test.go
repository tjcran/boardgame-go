package mcp

import (
	"context"
	"errors"
	"os"
	"testing"
)

// pgDSN returns the test DSN or skips. Same env var as storage/postgres
// tests so a single Postgres instance covers both packages:
//
//	docker run --rm -p 5432:5432 -e POSTGRES_PASSWORD=test postgres:16
//	BOARDGAMEGO_POSTGRES_DSN='postgres://postgres:test@localhost:5432/postgres?sslmode=disable' \
//	    go test ./mcp/... ./storage/postgres/...
func pgDSN(t *testing.T) string {
	t.Helper()
	d := os.Getenv("BOARDGAMEGO_POSTGRES_DSN")
	if d == "" {
		t.Skip("set BOARDGAMEGO_POSTGRES_DSN to run Postgres ownership tests")
	}
	return d
}

// openPGAndReset opens a fresh PostgresOwnership and truncates the
// ownership table so each test starts from a clean slate.
func openPGAndReset(t *testing.T) *PostgresOwnership {
	t.Helper()
	o, err := OpenPostgresOwnership(pgDSN(t))
	if err != nil {
		t.Fatalf("OpenPostgresOwnership: %v", err)
	}
	t.Cleanup(func() { _ = o.Close() })
	if _, err := o.db.Exec(`TRUNCATE TABLE match_ownership`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	return o
}

func TestPG_ClaimAndOwns(t *testing.T) {
	o := openPGAndReset(t)
	ctx := context.Background()

	if err := o.Claim(ctx, "alice", "match-1"); err != nil {
		t.Fatalf("Claim: %v", err)
	}
	owns, err := o.Owns(ctx, "alice", "match-1")
	if err != nil || !owns {
		t.Errorf("alice should own match-1: owns=%v err=%v", owns, err)
	}
	owns, _ = o.Owns(ctx, "bob", "match-1")
	if owns {
		t.Errorf("bob should not own match-1")
	}
}

func TestPG_DoubleClaimByOtherUserFails(t *testing.T) {
	o := openPGAndReset(t)
	ctx := context.Background()
	if err := o.Claim(ctx, "alice", "match-1"); err != nil {
		t.Fatalf("first claim: %v", err)
	}
	if err := o.Claim(ctx, "bob", "match-1"); err == nil {
		t.Error("expected error when a second user claims the same match")
	}
}

func TestPG_IdempotentReclaim(t *testing.T) {
	o := openPGAndReset(t)
	ctx := context.Background()
	if err := o.Claim(ctx, "alice", "match-1"); err != nil {
		t.Fatalf("first claim: %v", err)
	}
	if err := o.Claim(ctx, "alice", "match-1"); err != nil {
		t.Errorf("reclaim by same user should be idempotent, got %v", err)
	}
	matches, _ := o.MatchesFor(ctx, "alice")
	if len(matches) != 1 {
		t.Errorf("expected 1 match, got %d (%v)", len(matches), matches)
	}
}

func TestPG_MatchesForOrderedByCreation(t *testing.T) {
	o := openPGAndReset(t)
	ctx := context.Background()
	_ = o.Claim(ctx, "alice", "m1")
	_ = o.Claim(ctx, "alice", "m2")
	_ = o.Claim(ctx, "bob", "m3")

	aliceMatches, _ := o.MatchesFor(ctx, "alice")
	if len(aliceMatches) != 2 || aliceMatches[0] != "m1" || aliceMatches[1] != "m2" {
		t.Errorf("alice matches = %v, want [m1 m2]", aliceMatches)
	}
	bobMatches, _ := o.MatchesFor(ctx, "bob")
	if len(bobMatches) != 1 || bobMatches[0] != "m3" {
		t.Errorf("bob matches = %v, want [m3]", bobMatches)
	}
}

func TestPG_MissingMatchIDOrUserIDRejected(t *testing.T) {
	o := openPGAndReset(t)
	ctx := context.Background()
	if err := o.Claim(ctx, "", "match-1"); err == nil {
		t.Error("expected error on empty userID")
	}
	if err := o.Claim(ctx, "alice", ""); err == nil {
		t.Error("expected error on empty matchID")
	}
}

// TestPG_ToolIntegration drives the full Tools surface against a Postgres
// store, asserting cross-user rejection survives through the tool layer.
func TestPG_ToolIntegration(t *testing.T) {
	o := openPGAndReset(t)
	tools := newOwnedToolsForTest(t)
	tools.Ownership = o // swap the memory store for Postgres

	aliceCtx := WithUserID(context.Background(), "alice")
	bobCtx := WithUserID(context.Background(), "bob")

	created, err := tools.CreateMatch(aliceCtx, CreateMatchArgs{Game: "tic-tac-toe", NumPlayers: 2})
	if err != nil {
		t.Fatalf("alice create: %v", err)
	}
	if _, err := tools.JoinMatch(bobCtx, JoinMatchArgs{MatchID: created.MatchID, PlayerID: "0"}); err == nil {
		t.Error("expected cross-user join to fail")
	} else if !errors.Is(err, ErrNotOwned) {
		t.Errorf("expected ErrNotOwned, got %v", err)
	}
	if _, err := tools.JoinMatch(aliceCtx, JoinMatchArgs{MatchID: created.MatchID, PlayerID: "0"}); err != nil {
		t.Errorf("alice should be able to join her own match: %v", err)
	}
}
