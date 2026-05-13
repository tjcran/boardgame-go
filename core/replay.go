package core

import "fmt"

// Replay reconstructs the State of a match by re-applying every recorded
// move log entry from a fresh NewMatch. The result should byte-match the
// state at the time the log was captured, modulo plugin private data
// that PlayerView stripped (e.g. the Random PRNG state isn't recoverable
// without the original seed in plugin data).
//
// Useful for debugging (re-run a player's reported sequence against a
// new build of the game), audit trails (verify a final score was reached
// legally), and AI corpora (replay matches into a training pipeline).
//
// BGIO can't promise this — its log + replay path leaks wall-clock and
// Math.random calls that aren't gated by a plugin. Our pure reducer
// makes replay byte-deterministic given the same seed.
//
// log should be the full sequence of "move"-kind entries, in order. Custom
// AddLog entries and engine event entries are skipped.
func Replay(game *Game, log []LogEntry, numPlayers int, setupData any) (State, error) {
	state := NewMatch(game, numPlayers, setupData)
	for i, e := range log {
		if e.Kind != "move" {
			continue
		}
		next, err := Apply(game, state, MoveRequest{
			PlayerID: e.PlayerID,
			Move:     e.Move,
			Args:     e.Args,
		})
		if err != nil {
			return state, fmt.Errorf("replay step %d (%s by %s): %w", i, e.Move, e.PlayerID, err)
		}
		state = next
	}
	return state, nil
}
