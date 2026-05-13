package match

import (
	"testing"
	"time"

	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/storage"
)

// fakeClock returns a time.Time we can advance from the test.
type fakeClock struct{ now time.Time }

func (f *fakeClock) Now() time.Time { return f.now }

func TestJanitorWipesIdleMatches(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1_000_000, 0)}
	m := NewManager(storage.NewMemory())
	m.now = clock.Now
	m.MustRegister(pingPongGame())

	id, err := m.Create("pp", CreateOptions{})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Advance the clock past the idle window.
	clock.now = clock.now.Add(2 * time.Hour)

	m.janitorSweep(JanitorOptions{IdleAfter: time.Hour})

	if _, err := m.State(id); err != storage.ErrNotFound {
		t.Fatalf("expected match wiped, got %v", err)
	}
}

func TestJanitorKeepsFreshMatches(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1_000_000, 0)}
	m := NewManager(storage.NewMemory())
	m.now = clock.Now
	m.MustRegister(pingPongGame())

	id, _ := m.Create("pp", CreateOptions{})

	clock.now = clock.now.Add(5 * time.Minute)
	m.janitorSweep(JanitorOptions{IdleAfter: time.Hour})

	if _, err := m.State(id); err != nil {
		t.Fatalf("expected fresh match preserved, got %v", err)
	}
}

func TestJanitorWipesFinishedMatchesSooner(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1_000_000, 0)}
	m := NewManager(storage.NewMemory())
	m.now = clock.Now
	m.MustRegister(pingPongGame())

	id, _ := m.Create("pp", CreateOptions{})
	// Mark the match gameover artificially through storage so we can
	// exercise the FinishedAfter path without playing the game out.
	mm, _ := m.State(id)
	mm.State.Ctx.Gameover = map[string]any{"draw": true}
	_ = m.store.Update(mm)

	clock.now = clock.now.Add(45 * time.Minute)
	m.janitorSweep(JanitorOptions{
		IdleAfter:     2 * time.Hour,
		FinishedAfter: 30 * time.Minute,
	})

	if _, err := m.State(id); err != storage.ErrNotFound {
		t.Fatalf("finished match should have been wiped early, got err=%v", err)
	}
}

// silence unused-import lint on core in this file
var _ = core.Ctx{}
