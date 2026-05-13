package storage

import (
	"context"
	"errors"
	"sync"
	"time"
)

// WriteBehindOpts configures NewWriteBehind.
type WriteBehindOpts struct {
	// BatchSize triggers a flush when the pending queue reaches this many
	// distinct matches. Zero falls back to FlushInterval-only.
	BatchSize int
	// FlushInterval is the maximum time a pending write waits before
	// being flushed. Zero falls back to BatchSize-only (a queue can
	// never be left waiting if BatchSize is also zero, so at least one
	// of the two must be set).
	FlushInterval time.Duration
	// OnFlushError is called on every per-match flush error so callers
	// can plumb retries / alerting. Nil = ignore.
	OnFlushError func(matchID string, err error)
}

// WriteBehind wraps a Storage with asynchronous batched Update flushes.
// Update returns immediately after enqueueing; the actual write happens
// in a background goroutine. Pending Updates for the same match
// coalesce — only the latest payload per match is flushed.
//
// Get / List / Create / Wipe pass through synchronously. Get observes
// pending writes (returns the in-flight version, falling back to the
// inner storage if no pending write).
//
// Trade: durability. A server crash drops up to one BatchSize / one
// FlushInterval window of state. Use only when your game can tolerate
// a few seconds of work loss — or pair with a periodic Sync() call.
type WriteBehind struct {
	inner Storage
	opts  WriteBehindOpts

	mu      sync.Mutex
	pending map[string]*Match

	flushReq chan struct{}
	stopOnce sync.Once
	stop     chan struct{}
	done     chan struct{}
}

// ErrWriteBehindRequiresBatchOrInterval is returned by NewWriteBehind
// when neither BatchSize nor FlushInterval is set.
var ErrWriteBehindRequiresBatchOrInterval = errors.New(
	"WriteBehind: either BatchSize or FlushInterval must be > 0")

// NewWriteBehind constructs a write-behind decorator and starts its
// background flusher.
func NewWriteBehind(inner Storage, opts WriteBehindOpts) (*WriteBehind, error) {
	if opts.BatchSize <= 0 && opts.FlushInterval <= 0 {
		return nil, ErrWriteBehindRequiresBatchOrInterval
	}
	w := &WriteBehind{
		inner:    inner,
		opts:     opts,
		pending:  map[string]*Match{},
		flushReq: make(chan struct{}, 1),
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
	go w.flushLoop()
	return w, nil
}

// Close stops the flusher after flushing whatever remains. Idempotent.
func (w *WriteBehind) Close(ctx context.Context) error {
	w.stopOnce.Do(func() { close(w.stop) })
	select {
	case <-w.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Sync forces a flush of every currently-pending Update and waits for
// completion. Useful before shutdown or as a periodic durability gate.
func (w *WriteBehind) Sync() {
	w.flushOnce()
}

func (w *WriteBehind) Create(m *Match) error { return w.inner.Create(m) }

func (w *WriteBehind) Get(id string) (*Match, error) {
	w.mu.Lock()
	if m, ok := w.pending[id]; ok {
		w.mu.Unlock()
		return m, nil
	}
	w.mu.Unlock()
	return w.inner.Get(id)
}

// Update enqueues; coalesces with any earlier pending entry for the same
// match.
func (w *WriteBehind) Update(m *Match) error {
	w.mu.Lock()
	w.pending[m.ID] = m
	full := w.opts.BatchSize > 0 && len(w.pending) >= w.opts.BatchSize
	w.mu.Unlock()
	if full {
		w.kick()
	}
	return nil
}

func (w *WriteBehind) List(gameName string) ([]*Match, error) {
	// Flush first so List reflects pending writes. This is a coarse
	// guarantee — concurrent Updates after List starts will still race
	// — but it avoids stale data in the common "admin lists matches"
	// case.
	w.flushOnce()
	return w.inner.List(gameName)
}

func (w *WriteBehind) Wipe(id string) error {
	w.mu.Lock()
	delete(w.pending, id)
	w.mu.Unlock()
	return w.inner.Wipe(id)
}

// kick wakes the flush loop without blocking.
func (w *WriteBehind) kick() {
	select {
	case w.flushReq <- struct{}{}:
	default:
	}
}

// flushLoop drains the pending map on every kick or interval tick.
func (w *WriteBehind) flushLoop() {
	defer close(w.done)
	var tickC <-chan time.Time
	if w.opts.FlushInterval > 0 {
		t := time.NewTicker(w.opts.FlushInterval)
		defer t.Stop()
		tickC = t.C
	}
	for {
		select {
		case <-w.stop:
			w.flushOnce()
			return
		case <-w.flushReq:
			w.flushOnce()
		case <-tickC:
			w.flushOnce()
		}
	}
}

// flushOnce drains the pending map into a local slice and writes each
// match through. Errors are reported via OnFlushError; the loop never
// stops on a single bad flush.
func (w *WriteBehind) flushOnce() {
	w.mu.Lock()
	batch := w.pending
	w.pending = map[string]*Match{}
	w.mu.Unlock()
	for id, m := range batch {
		if err := w.inner.Update(m); err != nil {
			if w.opts.OnFlushError != nil {
				w.opts.OnFlushError(id, err)
			}
		}
	}
}
