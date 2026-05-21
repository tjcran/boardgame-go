// Package rps is a rock-paper-scissors game built on the boardgame-go
// engine. It is the reference example for *simultaneous actions*: every
// player submits a hidden throw, and once all have submitted the round
// resolves atomically and the throws are revealed together.
//
// The pattern composes from existing engine primitives — no engine changes
// are needed:
//
//   - Turn.ActivePlayers = ActivePlayersAllOnce makes every player active
//     with a one-move budget, so the engine removes each player from the
//     active set as soon as they submit (core/stage.go, core/reducer.go).
//   - The submit move buffers each throw in G.Pending, keyed by player.
//     PlayerView hides other seats' pending throws, and Move.Redact keeps
//     the chosen throw out of the move log for other seats.
//   - When the last player submits (len(Pending) == NumPlayers), submit
//     queues a server-only "reveal" move via mc.Queue.Push. It drains in
//     the same reducer pass as the final submission, so the round resolves
//     in a single atomic state update.
//   - reveal scores the round, clears the buffer, and either ends the game
//     or re-arms the next round with events.SetActivePlayers.
package rps

import (
	"fmt"

	"github.com/tjcran/boardgame-go/core"
)

// winTarget is the cumulative score a player must reach to win the match. A
// throw scores one point per opponent throw it beats, so this generalises to
// any player count.
const winTarget = 5

// State is the rock-paper-scissors G payload.
type State struct {
	// Pending holds each player's submitted throw for the current round,
	// keyed by player ID. It is hidden from other seats by PlayerView and
	// cleared on reveal. A non-empty entry means that player has locked in.
	Pending map[string]string `json:"pending"`

	// Scores is the public cumulative score per player.
	Scores map[string]int `json:"scores"`

	// LastRound is the throws from the most recently revealed round, kept
	// for display. Empty until the first reveal.
	LastRound map[string]string `json:"lastRound"`

	// Round is the 0-based index of the round currently being played.
	Round int `json:"round"`
}

// New returns the registered Game definition. Pass to a match.Manager.
func New() *core.Game {
	return &core.Game{
		Name:       "rock-paper-scissors",
		MinPlayers: 2,
		MaxPlayers: 4,
		Setup: func(_ core.Ctx, _ any) core.G {
			return &State{
				Pending:   map[string]string{},
				Scores:    map[string]int{},
				LastRound: map[string]string{},
			}
		},
		Moves: map[string]any{
			// submit is redacted so a player's throw never appears in the
			// move log seen by other seats.
			"submit": core.Move{Move: submit, Redact: true},
			// reveal is server-driven only: clients can't trigger the
			// resolution directly; submit queues it when the round is full.
			"reveal": core.Move{Move: reveal, ServerOnly: true, NoLimit: true},
		},
		// Every player is active with a one-move budget each round. The
		// engine removes a player from the active set after they submit,
		// and re-arms the set on reveal (see reveal).
		Turn: &core.TurnConfig{
			ActivePlayers: &core.ActivePlayersAllOnce,
		},
		PlayerView: playerView,
		Enumerate:  enumerate,
	}
}

var validThrows = map[string]bool{"rock": true, "paper": true, "scissors": true}

// beats reports whether throw a defeats throw b.
func beats(a, b string) bool {
	switch a {
	case "rock":
		return b == "scissors"
	case "paper":
		return b == "rock"
	case "scissors":
		return b == "paper"
	}
	return false
}

// submit records the calling player's hidden throw. When it completes the
// round (every player has submitted), it queues the server-only reveal move
// so the round resolves in the same reducer pass.
func submit(mc *core.MoveContext, args ...any) (core.G, error) {
	if len(args) < 1 {
		return mc.G, fmt.Errorf("%w: missing throw", core.ErrInvalidMove)
	}
	throw, ok := args[0].(string)
	if !ok || !validThrows[throw] {
		return mc.G, fmt.Errorf("%w: throw must be rock, paper, or scissors", core.ErrInvalidMove)
	}

	s := mc.G.(*State)
	next := s.clone()
	next.Pending[mc.PlayerID] = throw

	// Last to submit closes the round: queue the reveal. It runs as a
	// server-driven cascade step after the active set drains, in the same
	// reducer pass — so submit + reveal land as one atomic state update.
	if len(next.Pending) == mc.Ctx.NumPlayers {
		mc.Queue.Push("", "reveal")
	}
	return next, nil
}

// reveal resolves the round: it scores each player by how many opponents
// they beat, clears the hidden buffer, and either ends the match or re-arms
// the active set for the next round. Server-only — queued by submit.
func reveal(mc *core.MoveContext, _ ...any) (core.G, error) {
	s := mc.G.(*State)
	next := s.clone()

	// Score: one point per opponent you beat this round.
	for p, throw := range next.Pending {
		for q, other := range next.Pending {
			if p != q && beats(throw, other) {
				next.Scores[p]++
			}
		}
	}

	next.LastRound = map[string]string{}
	for p, throw := range next.Pending {
		next.LastRound[p] = throw
	}
	next.Pending = map[string]string{}
	next.Round++

	if winner := leader(next.Scores); winner != "" {
		mc.Events.EndGame(map[string]any{"winner": winner, "scores": next.Scores})
	} else {
		// Re-arm: every player active again for the next round.
		mc.Events.SetActivePlayers(core.ActivePlayersAllOnce)
	}
	return next, nil
}

// leader returns the single player who has reached winTarget, or "" if none
// has (or more than one tie at/above the target — then play continues).
func leader(scores map[string]int) string {
	winner := ""
	for p, n := range scores {
		if n >= winTarget {
			if winner != "" {
				return "" // multiple at target: no sole winner yet
			}
			winner = p
		}
	}
	return winner
}

// enumerate lists the three throws as legal submit moves for any player who
// is currently active and hasn't yet submitted this round.
func enumerate(g core.G, ctx core.Ctx, playerID string) []core.EnumerateAction {
	if ctx.ActivePlayers == nil {
		return nil
	}
	if _, active := ctx.ActivePlayers[playerID]; !active {
		return nil
	}
	s := g.(*State)
	if _, done := s.Pending[playerID]; done {
		return nil
	}
	return []core.EnumerateAction{
		{Move: "submit", Args: []any{"rock"}},
		{Move: "submit", Args: []any{"paper"}},
		{Move: "submit", Args: []any{"scissors"}},
	}
}

// playerView hides other players' pending throws. Each seat sees only its
// own locked-in throw (so a client can show "you picked rock, waiting…");
// spectators (empty playerID) see no pending throws. Everything else —
// scores, the last revealed round, the round number — is public.
func playerView(g core.G, _ core.Ctx, playerID string) core.G {
	s := g.(*State)
	view := s.clone()
	mine := map[string]string{}
	if playerID != "" {
		if throw, ok := s.Pending[playerID]; ok {
			mine[playerID] = throw
		}
	}
	view.Pending = mine
	return view
}

// clone returns a deep copy so moves and views never mutate shared maps.
func (s *State) clone() *State {
	cp := &State{
		Pending:   make(map[string]string, len(s.Pending)),
		Scores:    make(map[string]int, len(s.Scores)),
		LastRound: make(map[string]string, len(s.LastRound)),
		Round:     s.Round,
	}
	for k, v := range s.Pending {
		cp.Pending[k] = v
	}
	for k, v := range s.Scores {
		cp.Scores[k] = v
	}
	for k, v := range s.LastRound {
		cp.LastRound[k] = v
	}
	return cp
}
