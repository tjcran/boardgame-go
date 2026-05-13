package core

// LogEntry records one applied move (or event-fired transition) in the
// match log. The log lets clients reconstruct history, supports undo within
// the current turn, and feeds future replay / spectator features.
//
// Mirrors boardgame.io's log entries.
type LogEntry struct {
	// Kind is "move" for player moves, or one of the event types
	// ("endTurn", "endPhase", …) when a transition was queued explicitly.
	Kind string `json:"kind"`

	// Move is the move name (empty for non-move entries).
	Move string `json:"move,omitempty"`

	// PlayerID who initiated this action. Empty for engine-driven
	// transitions (e.g. auto-end-turn on MaxMoves).
	PlayerID string `json:"playerID,omitempty"`

	// Args are the move's arguments. When redact applies, this slice is
	// nilified per-seat at the moment of PlayerView; the in-memory entry
	// retains the original args.
	Args []any `json:"args,omitempty"`

	// Turn at the time this entry was written.
	Turn int `json:"turn"`

	// Phase at the time this entry was written.
	Phase string `json:"phase,omitempty"`

	// Redact records whether this entry is hidden from non-initiating
	// players. Resolved at write time so subsequent state changes don't
	// alter the verdict.
	Redact bool `json:"redact,omitempty"`

	// Undoable records whether this entry can be the target of an undo
	// in the current turn. Mirrors BGIO's Move.Undoable resolution at the
	// time the move was played.
	Undoable bool `json:"undoable"`

	// Parent is the index in State.Log of the external move that
	// initiated this cascade entry. -1 means "external" (top-level).
	// Drain steps and resume entries point at their initiating move so
	// clients can render hierarchical move logs.
	Parent int `json:"parent,omitempty"`

	// ResumeTag, on a move entry, is the value of MoveRequest.ResumeTag
	// that resolved a pending block. Empty for non-resume moves.
	ResumeTag string `json:"resumeTag,omitempty"`
}

// redactedLog returns the log filtered for a specific seat: entries marked
// Redact have their Args nilified for everyone except the original player
// (or the server, when playerID is the server-side rendered view).
func redactedLog(log []LogEntry, playerID string) []LogEntry {
	out := make([]LogEntry, len(log))
	for i, e := range log {
		out[i] = e
		if e.Redact && e.PlayerID != playerID {
			out[i].Args = nil
		}
	}
	return out
}
