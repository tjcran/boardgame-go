package mcp

import (
	"context"
	"testing"
)

func runUserGameStoreSuite(t *testing.T, s UserGameStore) {
	ctx := context.Background()

	if err := s.Put(ctx, UserGame{UserID: "alice", Name: "hex", Source: "META={...}", LLMGuide: "play center"}); err != nil {
		t.Fatalf("Put alice/hex: %v", err)
	}
	if err := s.Put(ctx, UserGame{UserID: "bob", Name: "hex", Source: "META={...}"}); err != nil {
		t.Fatalf("Put bob/hex: %v", err)
	}

	got, err := s.Get(ctx, "alice", "hex")
	if err != nil || got == nil { t.Fatalf("Get alice/hex: %v %v", got, err) }
	if got.LLMGuide != "play center" { t.Errorf("LLMGuide: %q", got.LLMGuide) }

	names, err := s.List(ctx, "alice")
	if err != nil || len(names) != 1 || names[0] != "hex" {
		t.Errorf("List alice: %v %v", names, err)
	}

	if err := s.Delete(ctx, "alice", "hex"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	got, _ = s.Get(ctx, "alice", "hex")
	if got != nil { t.Errorf("expected nil after Delete, got %v", got) }

	// bob's row untouched.
	got, _ = s.Get(ctx, "bob", "hex")
	if got == nil { t.Errorf("bob/hex should still exist") }
}

func TestInMemoryUserGames(t *testing.T) {
	runUserGameStoreSuite(t, NewInMemoryUserGames())
}
