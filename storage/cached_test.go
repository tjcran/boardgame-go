package storage

import (
	"testing"
	"time"

	"github.com/tjcran/boardgame-go/core"
)

// countingStore wraps Memory and counts every call to verify the cache
// is actually intercepting reads.
type countingStore struct {
	inner *Memory
	gets  int
}

func (c *countingStore) Create(m *Match) error            { return c.inner.Create(m) }
func (c *countingStore) Update(m *Match) error            { return c.inner.Update(m) }
func (c *countingStore) List(name string) ([]*Match, error) { return c.inner.List(name) }
func (c *countingStore) Wipe(id string) error             { return c.inner.Wipe(id) }
func (c *countingStore) Get(id string) (*Match, error) {
	c.gets++
	return c.inner.Get(id)
}

func cachedTestMatch(id string) *Match {
	return &Match{
		ID:       id,
		GameName: "test",
		State:    core.State{G: map[string]any{"x": 1}},
		Players:  []Player{{ID: "0", Seat: "0"}},
	}
}

func TestCachedHitsCacheOnRepeatGet(t *testing.T) {
	cs := &countingStore{inner: NewMemory()}
	c := NewCached(cs, CacheOpts{TTL: time.Hour, MaxEntries: 100})
	m := cachedTestMatch("a")
	_ = c.Create(m)

	for i := 0; i < 5; i++ {
		if _, err := c.Get("a"); err != nil {
			t.Fatalf("get %d: %v", i, err)
		}
	}
	// Create already seeds the cache, so zero passthrough Gets expected.
	if cs.gets != 0 {
		t.Fatalf("expected 0 inner Gets (all cache hits), got %d", cs.gets)
	}
}

func TestCachedExpiresAfterTTL(t *testing.T) {
	clock := time.Unix(1000, 0)
	cs := &countingStore{inner: NewMemory()}
	c := NewCached(cs, CacheOpts{
		TTL:        100 * time.Millisecond,
		MaxEntries: 100,
		Now:        func() time.Time { return clock },
	})
	_ = c.Create(cachedTestMatch("a"))
	_, _ = c.Get("a") // cache hit

	clock = clock.Add(time.Second) // past TTL
	_, _ = c.Get("a")              // forced reload from inner
	if cs.gets != 1 {
		t.Fatalf("expected 1 inner Get after TTL expiry, got %d", cs.gets)
	}
}

func TestCachedEvictsAtMaxEntries(t *testing.T) {
	cs := &countingStore{inner: NewMemory()}
	c := NewCached(cs, CacheOpts{MaxEntries: 2})
	_ = c.Create(cachedTestMatch("a"))
	_ = c.Create(cachedTestMatch("b"))
	_ = c.Create(cachedTestMatch("c")) // evicts "a"
	_, _ = c.Get("a")                  // forced reload
	if cs.gets != 1 {
		t.Fatalf("expected 1 reload after eviction, got %d", cs.gets)
	}
}

func TestCachedUpdateRefreshesEntry(t *testing.T) {
	cs := &countingStore{inner: NewMemory()}
	c := NewCached(cs, CacheOpts{TTL: time.Hour, MaxEntries: 100})
	m := cachedTestMatch("a")
	_ = c.Create(m)

	m.State.G = map[string]any{"x": 99}
	_ = c.Update(m)
	got, _ := c.Get("a")
	if got.State.G.(map[string]any)["x"] != 99 {
		t.Fatalf("expected cached value to reflect Update, got %+v", got.State.G)
	}
}

func TestCachedInvalidateClearsEntry(t *testing.T) {
	cs := &countingStore{inner: NewMemory()}
	c := NewCached(cs, CacheOpts{TTL: time.Hour, MaxEntries: 100})
	_ = c.Create(cachedTestMatch("a"))
	c.Invalidate("a")
	_, _ = c.Get("a")
	if cs.gets != 1 {
		t.Fatalf("expected 1 reload after Invalidate, got %d", cs.gets)
	}
}
