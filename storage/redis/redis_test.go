package redis_test

import (
	"testing"

	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"

	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/storage"
	redisstore "github.com/tjcran/boardgame-go/storage/redis"
)

func openTest(t *testing.T) (*redisstore.Storage, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)
	client := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	s, err := redisstore.New(redisstore.Options{Client: client})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return s, mr
}

func makeMatch(id, game string) *storage.Match {
	return &storage.Match{
		ID:       id,
		GameName: game,
		State: core.State{
			G:   map[string]any{"score": 1},
			Ctx: core.Ctx{NumPlayers: 2, CurrentPlayer: "0", PlayOrder: []string{"0", "1"}, Turn: 1},
		},
		Players:   []storage.Player{{ID: "0", Seat: "0", Name: "alice"}},
		CreatedAt: 1234,
	}
}

func TestRedisRoundTrip(t *testing.T) {
	s, _ := openTest(t)
	m := makeMatch("abc", "ttt")
	if err := s.Create(m); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := s.Get("abc")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Players[0].Name != "alice" || got.GameName != "ttt" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}

func TestRedisCreateRejectsDuplicateID(t *testing.T) {
	s, _ := openTest(t)
	m := makeMatch("dup", "ttt")
	if err := s.Create(m); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := s.Create(m); err == nil {
		t.Fatal("expected duplicate-id error on second Create, got nil")
	}
}

func TestRedisUpdateRequiresExisting(t *testing.T) {
	s, _ := openTest(t)
	if err := s.Update(makeMatch("missing", "ttt")); err != storage.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestRedisListFiltersByGame(t *testing.T) {
	s, _ := openTest(t)
	_ = s.Create(makeMatch("a1", "ttt"))
	_ = s.Create(makeMatch("a2", "ttt"))
	_ = s.Create(makeMatch("b1", "chess"))

	got, err := s.List("ttt")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 ttt matches, got %d", len(got))
	}
	all, err := s.List("")
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3 matches in all-games list, got %d", len(all))
	}
}

func TestRedisWipeRemovesFromIndices(t *testing.T) {
	s, mr := openTest(t)
	_ = s.Create(makeMatch("x", "ttt"))
	if err := s.Wipe("x"); err != nil {
		t.Fatalf("wipe: %v", err)
	}
	if _, err := s.Get("x"); err != storage.ErrNotFound {
		t.Fatalf("expected ErrNotFound after wipe, got %v", err)
	}
	// Indices were trimmed: per-game set and the all-set should not contain "x".
	gameSet, _ := mr.SMembers("bgio:matches:ttt")
	if contains(gameSet, "x") {
		t.Fatalf("expected x removed from per-game set, got %v", gameSet)
	}
	allSet, _ := mr.SMembers("bgio:matches:*all*")
	if contains(allSet, "x") {
		t.Fatalf("expected x removed from all-set, got %v", allSet)
	}
}

func contains(set []string, want string) bool {
	for _, s := range set {
		if s == want {
			return true
		}
	}
	return false
}

func TestRedisCustomPrefix(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer mr.Close()
	client := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	defer client.Close()
	s, err := redisstore.New(redisstore.Options{Client: client, Prefix: "test:"})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if err := s.Create(makeMatch("z", "ttt")); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := mr.Get("test:match:z"); err != nil {
		t.Fatalf("expected key under custom prefix, got %v", err)
	}
}
