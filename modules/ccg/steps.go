package ccg

import "errors"

// Step sequences: ordered, named steps with optional response windows.
//
// Many games run a sub-procedure (a combat, a turn segment, a
// resolution round) as a fixed list of steps, where some steps give
// every player a chance to respond before the procedure moves on.
// StepSequence is the bookkeeping for that and nothing else: which
// sequence is running, which step is current, and whether that step
// calls for a response window. It lives in the game's G and
// serializes with it.
//
// It deliberately does not import or drive PriorityState. The game
// supplies step-entry behavior and decides when a closed window
// advances the step. The canonical loop:
//
//	// entering a step:
//	if cur, ok := seq.Current(); ok && cur.Window {
//		// open a PriorityState window for this step
//	}
//
//	// when the window closes:
//	if len(g.Pending) == 0 {
//		next, done, err := seq.Advance()
//		// done: the sequence is over; otherwise enter `next`
//	} else {
//		// effects were put on the queue during the window:
//		// resolve them, then reopen a window in the SAME step
//	}
//
// A step with Window false is entered, acted on by the game, and
// advanced immediately.

var (
	// ErrSequenceActive is returned by Begin when a sequence is
	// already running.
	ErrSequenceActive = errors.New("ccg: step sequence already active")
	// ErrSequenceEmpty is returned by Begin when given no steps.
	ErrSequenceEmpty = errors.New("ccg: step sequence has no steps")
	// ErrSequenceInactive is returned by Advance when no sequence is
	// running.
	ErrSequenceInactive = errors.New("ccg: step sequence not active")
)

// StepDef names one step and says whether it opens a response window.
type StepDef struct {
	Name   string `json:"name"`
	Window bool   `json:"window,omitempty"`
}

// StepSequence tracks progress through a list of steps. The zero
// value is inactive and serializes to nothing under `omitzero`.
type StepSequence struct {
	Name   string    `json:"name,omitempty"`
	Steps  []StepDef `json:"steps,omitempty"`
	Index  int       `json:"index,omitempty"`
	Active bool      `json:"active,omitempty"`
}

// Begin starts a sequence at its first step. The steps slice is
// copied, so later caller mutation cannot alter a running sequence.
func (s *StepSequence) Begin(name string, steps []StepDef) error {
	if s.Active {
		return ErrSequenceActive
	}
	if len(steps) == 0 {
		return ErrSequenceEmpty
	}
	s.Name = name
	s.Steps = append([]StepDef(nil), steps...)
	s.Index = 0
	s.Active = true
	return nil
}

// Current returns the current step; false when inactive.
func (s *StepSequence) Current() (StepDef, bool) {
	if !s.Active || s.Index < 0 || s.Index >= len(s.Steps) {
		return StepDef{}, false
	}
	return s.Steps[s.Index], true
}

// Is reports whether the sequence is active and at the named step.
func (s *StepSequence) Is(step string) bool {
	cur, ok := s.Current()
	return ok && cur.Name == step
}

// Advance moves to the next step. Past the last step it clears the
// sequence and returns done=true with a zero next.
func (s *StepSequence) Advance() (next StepDef, done bool, err error) {
	if !s.Active {
		return StepDef{}, false, ErrSequenceInactive
	}
	if s.Index+1 >= len(s.Steps) {
		s.Reset()
		return StepDef{}, true, nil
	}
	s.Index++
	return s.Steps[s.Index], false, nil
}

// Reset clears the sequence, abandoning any progress.
func (s *StepSequence) Reset() {
	*s = StepSequence{}
}
