package storage

import (
	"container/list"
	"sync"
	"time"
)

// CacheOpts configures NewCached.
type CacheOpts struct {
	// TTL is the per-entry lifetime. Zero disables time-based eviction.
	TTL time.Duration
	// MaxEntries caps the cache size. Zero disables size-based eviction.
	MaxEntries int
	// Now is a clock injection point for tests. Defaults to time.Now.
	Now func() time.Time
}

// Cached wraps any Storage with an in-memory write-through cache. Reads
// hit the cache; writes update the cache and forward to the inner
// storage synchronously. Replicates BGIO's server-keeps-hot-state model
// for users on slower backends (Postgres, Redis-over-WAN, etc.).
//
// The cache is keyed by match ID; List() bypasses the cache and queries
// the inner storage so result freshness matches an uncached fetch.
//
// Concurrency: safe. A short RWMutex covers the cache map; eviction is
// O(1) via a doubly-linked LRU list.
type Cached struct {
	inner Storage
	opts  CacheOpts

	mu    sync.Mutex
	cache map[string]*cacheEntry
	lru   *list.List
}

type cacheEntry struct {
	match  *Match
	expiry time.Time
	elem   *list.Element // back-reference into lru
}

// NewCached wraps inner with a TTL+LRU cache.
func NewCached(inner Storage, opts CacheOpts) *Cached {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &Cached{
		inner: inner,
		opts:  opts,
		cache: map[string]*cacheEntry{},
		lru:   list.New(),
	}
}

// Create writes through and seeds the cache.
func (c *Cached) Create(m *Match) error {
	if err := c.inner.Create(m); err != nil {
		return err
	}
	c.put(m)
	return nil
}

// Get returns the cached entry if fresh, otherwise loads from inner.
func (c *Cached) Get(id string) (*Match, error) {
	c.mu.Lock()
	if e, ok := c.cache[id]; ok {
		if c.opts.TTL == 0 || c.opts.Now().Before(e.expiry) {
			c.lru.MoveToFront(e.elem)
			m := e.match
			c.mu.Unlock()
			return m, nil
		}
		// Expired — drop and fall through.
		c.evictLocked(id)
	}
	c.mu.Unlock()

	m, err := c.inner.Get(id)
	if err != nil {
		return nil, err
	}
	c.put(m)
	return m, nil
}

// Update writes through and refreshes the cache.
func (c *Cached) Update(m *Match) error {
	if err := c.inner.Update(m); err != nil {
		return err
	}
	c.put(m)
	return nil
}

// List bypasses the cache. Cached results would have to be reconciled
// with inner state at every call, which defeats the purpose.
func (c *Cached) List(gameName string) ([]*Match, error) {
	return c.inner.List(gameName)
}

// Wipe removes from inner storage and invalidates the cache.
func (c *Cached) Wipe(id string) error {
	c.mu.Lock()
	c.evictLocked(id)
	c.mu.Unlock()
	return c.inner.Wipe(id)
}

// Invalidate drops the cached entry for id (if any). Used by external
// invalidators (cross-node cache bust). No-op if the entry isn't
// cached.
func (c *Cached) Invalidate(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.evictLocked(id)
}

// put inserts or refreshes a cache entry, evicting the oldest if the
// cap is reached.
func (c *Cached) put(m *Match) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.cache[m.ID]; ok {
		e.match = m
		if c.opts.TTL > 0 {
			e.expiry = c.opts.Now().Add(c.opts.TTL)
		}
		c.lru.MoveToFront(e.elem)
		return
	}
	entry := &cacheEntry{match: m}
	if c.opts.TTL > 0 {
		entry.expiry = c.opts.Now().Add(c.opts.TTL)
	}
	entry.elem = c.lru.PushFront(m.ID)
	c.cache[m.ID] = entry

	if c.opts.MaxEntries > 0 && c.lru.Len() > c.opts.MaxEntries {
		oldest := c.lru.Back()
		if oldest != nil {
			id := oldest.Value.(string)
			c.evictLocked(id)
		}
	}
}

// evictLocked removes an entry from cache + LRU. Caller holds c.mu.
func (c *Cached) evictLocked(id string) {
	if e, ok := c.cache[id]; ok {
		c.lru.Remove(e.elem)
		delete(c.cache, id)
	}
}
