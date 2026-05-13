// Package core is a minimal stub of github.com/tjcran/boardgame-go/core
// sufficient for the analyzer's structural checks. Real test fixtures
// live in cmd/boardgame-go-vet/internal/determinism/testdata.
package core

// G mirrors core.G.
type G = any

// MoveContext mirrors core.MoveContext (only the fields the analyzer
// looks at are populated).
type MoveContext struct {
	G      G
	Random *Random
}

// Random is the seeded PRNG handle.
type Random struct{}

func (*Random) D6() int { return 0 }
