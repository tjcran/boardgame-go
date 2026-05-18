package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/tjcran/boardgame-go/match"
	"github.com/tjcran/boardgame-go/storage"
)

func TestGuideResource_ListAndRead(t *testing.T) {
	mgr := match.NewManager(storage.NewMemory())
	store := NewInMemoryUserGames()
	reg := NewUserAwareRegistry(mgr, store)

	const src = `
META = {"name":"hex","min_players":2,"max_players":2}
def setup(ctx): return {}
MOVES = {"pass": {"args":[], "apply": lambda s, c: s}}
def end_if(state, ctx): return None
def legal_moves(state, ctx): return []
`
	if err := reg.RegisterUserGame(context.Background(), "alice", src, "play patiently"); err != nil {
		t.Fatalf("RegisterUserGame: %v", err)
	}

	// NewServer takes (info, instructions).
	srv := NewServer(ServerInfo{Name: "t", Version: "0"}, "")
	srv.WireGuideResources(reg)

	aliceCtx := contextWithUser(context.Background(), "alice")
	list, _ := srv.listGuideResources(aliceCtx)
	found := false
	for _, r := range list {
		if strings.HasSuffix(r.URI, "game://alice/hex/guide") {
			found = true
		}
	}
	if !found {
		t.Fatalf("list missing alice's guide: %v", list)
	}

	bobCtx := contextWithUser(context.Background(), "bob")
	bobList, _ := srv.listGuideResources(bobCtx)
	for _, r := range bobList {
		if strings.Contains(r.URI, "alice/hex") {
			t.Fatalf("bob saw alice's guide: %v", r)
		}
	}

	body, err := srv.readGuideResource(aliceCtx, "game://alice/hex/guide")
	if err != nil {
		t.Fatalf("readGuideResource: %v", err)
	}
	if !strings.Contains(body, "play patiently") {
		t.Fatalf("body missing guide content: %q", body)
	}

	_, err = srv.readGuideResource(bobCtx, "game://alice/hex/guide")
	if err == nil {
		t.Fatalf("bob got alice's body, expected error")
	}
}
