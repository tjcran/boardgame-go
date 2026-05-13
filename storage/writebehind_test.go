package storage

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// counting wraps Memory but tracks Update calls.
type updateCounter struct {
	inner   *Memory
	updates atomic.Int64
}

func (c *updateCounter) Create(m *Match) error            { return c.inner.Create(m) }
func (c *updateCounter) Get(id string) (*Match, error)    { return c.inner.Get(id) }
func (c *updateCounter) List(n string) ([]*Match, error)  { return c.inner.List(n) }
func (c *updateCounter) Wipe(id string) error             { return c.inner.Wipe(id) }
func (c *updateCounter) Update(m *Match) error {
	c.updates.Add(1)
	return c.inner.Update(m)
}

func TestWriteBehindCoalescesPerMatch(t *testing.T) {
	uc := &updateCounter{inner: NewMemory()}
	w, err := NewWriteBehind(uc, WriteBehindOpts{
		BatchSize:     1000,            // never trigger on size
		FlushInterval: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer w.Close(context.Background())

	m := cachedTestMatch("a")
	_ = w.Create(m)
	// 100 rapid-fire Updates against the same match should coalesce to
	// 1 inner Update on the next flush tick.
	for i := 0; i < 100; i++ {
		_ = w.Update(m)
	}
	time.Sleep(120 * time.Millisecond)
	got := uc.updates.Load()
	if got != 1 {
		t.Fatalf("expected 1 coalesced inner Update, got %d", got)
	}
}

func TestWriteBehindFlushesOnBatchSize(t *testing.T) {
	uc := &updateCounter{inner: NewMemory()}
	w, _ := NewWriteBehind(uc, WriteBehindOpts{
		BatchSize:     3,
		FlushInterval: 0, // size-only
	})
	defer w.Close(context.Background())

	for _, id := range []string{"a", "b", "c"} {
		m := cachedTestMatch(id)
		_ = w.Create(m)
		_ = w.Update(m)
	}
	// Hit the batch size — flush should run.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if uc.updates.Load() >= 3 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("expected 3 inner Updates after batch fill, got %d", uc.updates.Load())
}

func TestWriteBehindSyncDrainsBeforeReturning(t *testing.T) {
	uc := &updateCounter{inner: NewMemory()}
	w, _ := NewWriteBehind(uc, WriteBehindOpts{
		BatchSize:     1000, // never auto-flush
		FlushInterval: time.Hour,
	})
	defer w.Close(context.Background())
	m := cachedTestMatch("a")
	_ = w.Create(m)
	_ = w.Update(m)
	w.Sync()
	if uc.updates.Load() != 1 {
		t.Fatalf("Sync should have flushed pending; got %d Updates", uc.updates.Load())
	}
}

func TestWriteBehindGetReturnsPendingWrite(t *testing.T) {
	uc := &updateCounter{inner: NewMemory()}
	w, _ := NewWriteBehind(uc, WriteBehindOpts{
		BatchSize:     1000,
		FlushInterval: time.Hour, // never flush during the test
	})
	defer w.Close(context.Background())

	m := cachedTestMatch("a")
	_ = w.Create(m)
	m.State.G = map[string]any{"x": 999}
	_ = w.Update(m)

	got, _ := w.Get("a")
	if got.State.G.(map[string]any)["x"] != 999 {
		t.Fatalf("Get should observe pending Update, got %+v", got.State.G)
	}
}

func TestWriteBehindRejectsEmptyOpts(t *testing.T) {
	_, err := NewWriteBehind(NewMemory(), WriteBehindOpts{})
	if err == nil {
		t.Fatal("expected error when neither BatchSize nor FlushInterval is set")
	}
}
