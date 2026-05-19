package economy

import "github.com/tjcran/boardgame-go/modules/ccg"

// Pool is the per-player, per-kind resource handle. Pool itself holds
// no state — the counter values live in s.Counters(Owner, Kind). Keep
// the Pool config around for the duration of the game (or rebuild it
// from constants in your move handlers); it's a few words on the
// stack.
type Pool struct {
	// Owner is the player-entity ID whose counters the pool reads.
	// Typically a per-player entity registered at Setup.
	Owner ccg.EntityID
	// Kind is the counter key — pick a stable string per resource
	// ("gold", "actions", "buys").
	Kind string
	// Cap is the maximum value Gain and Set will clamp to. 0 means
	// uncapped — values may grow arbitrarily.
	Cap int
}

// Current returns the pool's current value. Wraps s.Counters; returns
// 0 for unknown Owner or unset counters.
func (p Pool) Current(s *ccg.State) int {
	return s.Counters(p.Owner, p.Kind)
}

// Gain adds n to the pool, clamped to Cap when Cap > 0. Returns the
// actual delta applied (0 when n ≤ 0 or when already at cap). The
// underlying ccg counter_changed event fires with the *applied* delta,
// matching ccg's Counter semantics.
func (p Pool) Gain(s *ccg.State, n int) int {
	if n <= 0 {
		return 0
	}
	if p.Cap > 0 {
		current := p.Current(s)
		room := p.Cap - current
		if room <= 0 {
			return 0
		}
		if n > room {
			n = room
		}
	}
	s.AddCounter(p.Owner, p.Kind, n)
	return n
}
