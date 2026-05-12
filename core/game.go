// Package core defines the pure game engine: types and a reducer that, given
// a Game definition and a current State, produces the next State in response
// to a Move. Nothing in this package touches I/O, time, or randomness — that
// keeps games deterministic and trivially testable.
package core

import "errors"

// G is the user-defined game state. Games store whatever they want here
// (board, scores, etc). Treat it as an opaque payload from the engine's
// perspective; the engine never inspects it.
type G = any

// Ctx is the engine-managed metadata that lives alongside G. It tells you
// whose turn it is, how many turns have happened, who's playing, and whether
// the game is over. Moves see Ctx but never mutate it directly — the reducer
// is responsible for advancing it.
type Ctx struct {
	NumPlayers    int      `json:"numPlayers"`
	CurrentPlayer string   `json:"currentPlayer"`
	PlayOrder     []string `json:"playOrder"`
	Turn          int      `json:"turn"`
	GameOver      bool     `json:"gameOver"`
	Winner        string   `json:"winner,omitempty"`
	IsDraw        bool     `json:"isDraw,omitempty"`
}

// State is the complete authoritative state of a match.
type State struct {
	G   G   `json:"G"`
	Ctx Ctx `json:"ctx"`
}

// MoveFn is the signature every move handler implements. It receives the
// current G and Ctx plus the move arguments and returns the next G. If the
// move is illegal it returns ErrInvalidMove (or any error) and G is unchanged.
//
// MoveFn must be deterministic for a given (G, Ctx, args). Convention: treat
// G as immutable — return a new value rather than mutating in place. The
// engine does not deep-copy for you.
type MoveFn func(g G, ctx Ctx, args ...any) (G, error)

// ErrInvalidMove is the conventional error a MoveFn returns when the move is
// disallowed by the rules. The reducer surfaces it without advancing state.
var ErrInvalidMove = errors.New("invalid move")

// Game is a declarative definition. The framework consumes one of these and
// runs matches against it.
type Game struct {
	// Name is the registration key used by transports (e.g. URL segment).
	Name string

	// MinPlayers/MaxPlayers bracket how many seats a match can have. If both
	// are zero, the engine treats the game as 2-player.
	MinPlayers int
	MaxPlayers int

	// Setup builds the initial G for a new match given the player count.
	Setup func(numPlayers int) G

	// Moves is the registry of legal move names -> handlers.
	Moves map[string]MoveFn

	// EndIf, if set, is called after each successful move. It can return a
	// winner string (one of ctx.PlayOrder) or signal a draw. Returning
	// (false, "", false) means the game continues.
	EndIf func(g G, ctx Ctx) (over bool, winner string, draw bool)
}

// playerCount returns the configured player count, defaulting to 2 if the
// game didn't specify. Used for Setup and for building the initial PlayOrder.
func (g *Game) playerCount(requested int) int {
	if requested > 0 {
		return requested
	}
	if g.MaxPlayers > 0 {
		return g.MaxPlayers
	}
	return 2
}
