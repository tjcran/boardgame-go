package match

import (
	"context"
	"time"
)

// JanitorOptions configures Manager.RunJanitor.
type JanitorOptions struct {
	// IdleAfter is how long a match can sit untouched before the janitor
	// wipes it. Matches with a non-zero ctx.Gameover are wiped after
	// FinishedAfter (defaults to IdleAfter / 2 if zero).
	IdleAfter time.Duration

	// FinishedAfter, if non-zero, overrides IdleAfter for matches that
	// have already ended. Defaults to IdleAfter / 2.
	FinishedAfter time.Duration

	// Tick is how often the janitor scans the store. Defaults to 5min.
	Tick time.Duration
}

// RunJanitor runs a background sweep that wipes idle matches. Returns
// when ctx is cancelled. Safe to run on multiple managers as long as they
// share storage — each call independently scans + wipes.
//
// BGIO has no equivalent; running such a loop in Node would compete with
// the engine event loop. Here it's just another goroutine.
func (m *Manager) RunJanitor(ctx context.Context, opts JanitorOptions) {
	if opts.IdleAfter <= 0 {
		opts.IdleAfter = 24 * time.Hour
	}
	if opts.FinishedAfter <= 0 {
		opts.FinishedAfter = opts.IdleAfter / 2
	}
	if opts.Tick <= 0 {
		opts.Tick = 5 * time.Minute
	}

	ticker := time.NewTicker(opts.Tick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.janitorSweep(opts)
		}
	}
}

// janitorSweep wipes one batch of stale matches. Extracted for testing.
func (m *Manager) janitorSweep(opts JanitorOptions) {
	now := m.now()
	matches, err := m.store.List("")
	if err != nil {
		m.Logger.Warn("janitor.list_failed", "err", err.Error())
		return
	}
	wiped := 0
	for _, mm := range matches {
		idle := time.Duration(now.Unix()-mm.CreatedAt) * time.Second
		threshold := opts.IdleAfter
		if mm.State.Ctx.Gameover != nil {
			threshold = opts.FinishedAfter
		}
		if idle < threshold {
			continue
		}
		if err := m.store.Wipe(mm.ID); err != nil {
			m.Logger.Warn("janitor.wipe_failed",
				"match_id", mm.ID, "err", err.Error())
			continue
		}
		wiped++
		m.Logger.Info("janitor.wiped",
			"match_id", mm.ID, "game", mm.GameName,
			"idle_sec", idle.Seconds(),
			"finished", mm.State.Ctx.Gameover != nil)
	}
	if wiped > 0 {
		m.Logger.Info("janitor.sweep", "wiped", wiped)
	}
}
