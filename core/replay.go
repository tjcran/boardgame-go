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
	return ReplayUntil(game, log, -1, numPlayers, setupData)
}

// ReplayUntil is the time-travel variant of Replay: re-apply the first
// `untilSteps` "move"-kind log entries and stop. -1 means "the whole
// log". Returns the state as it was after exactly that many moves had
// been applied — useful for debugging ("what did the board look like
// 3 turns before the bug?") and for log-stepping UIs (BGIO issue #892:
// "Stepping through log outside of debug?").
//
// Non-move entries (custom AddLog) are skipped but not counted.
func ReplayUntil(game *Game, log []LogEntry, untilSteps int, numPlayers int, setupData any) (State, error) {
	state := NewMatch(game, numPlayers, setupData)
	if untilSteps == 0 {
		return state, nil
	}
	step := 0
	for i, e := range log {
		if e.Kind != "move" {
			continue
		}
		next, err := Apply(game, state, MoveRequest{
			PlayerID:  e.PlayerID,
			Move:      e.Move,
			Args:      e.Args,
			ResumeTag: e.ResumeTag,
		})
		if err != nil {
			return state, fmt.Errorf("replay step %d (%s by %s): %w", i, e.Move, e.PlayerID, err)
		}
		state = next
		step++
		if untilSteps >= 0 && step >= untilSteps {
			break
		}
	}
	return state, nil
}
