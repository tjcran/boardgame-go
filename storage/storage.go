// Package storage defines the persistence boundary for matches. The match
// manager talks to a Storage and never opens files or DB handles directly,
// which lets us swap in memory/SQL/etc. without touching game logic.
package storage

import (
	"errors"

	"github.com/tjcran/boardgame-go/core"
)

// ErrNotFound is returned by Get when no match exists for the ID.
var ErrNotFound = errors.New("match not found")

// Player is a seated participant in a match.
type Player struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Seat        string `json:"seat"`        // seat index as string, matches ctx.PlayOrder
	Credentials string `json:"-"`           // never serialise to client output
	Data        any    `json:"data,omitempty"`
	IsConnected bool   `json:"isConnected,omitempty"`
}

// Match is the persisted shape of one in-progress (or finished) game.
type Match struct {
	ID          string     `json:"id"`
	GameName    string     `json:"gameName"`
	State       core.State `json:"state"`
	Players     []Player   `json:"players"` // indexed by seat
	SetupData   any        `json:"setupData,omitempty"`
	Unlisted    bool       `json:"unlisted,omitempty"`
	CreatedAt   int64      `json:"createdAt"`
	NextMatchID string     `json:"nextMatchID,omitempty"`

	// Name is an optional human-readable label set at create time
	// (e.g. "Friday Night Catan"). Surfaced via Lobby listings.
	Name string `json:"name,omitempty"`

	// JoinCode is an optional short code (6-8 chars) clients can give
	// to friends instead of the opaque match ID. Looked up via
	// GET /games/{gameName}/byCode/{code}. Addresses BGIO issue #574.
	JoinCode string `json:"joinCode,omitempty"`

	// SchemaVersion is the Game.SchemaVersion under which State was last
	// written. Read by Manager on load to decide whether to call
	// Game.Migrate. Defaults to 0 for legacy records — that's also the
	// default Game.SchemaVersion, so existing data round-trips cleanly.
	SchemaVersion int `json:"schemaVersion,omitempty"`
}

// Storage is the persistence interface the match manager depends on.
//
// All methods must be safe for concurrent use; the manager fans out HTTP and
// WebSocket requests without serialising at the call site. The shape
// mirrors BGIO's StorageAPI.Async surface (createMatch / setState /
// setMetadata / fetch / wipe / listMatches) collapsed into Go-idiomatic
// sync methods.
type Storage interface {
	Create(m *Match) error
	Get(id string) (*Match, error)
	Update(m *Match) error
	List(gameName string) ([]*Match, error)
	Wipe(id string) error
}

// ErrConflict is returned by OptimisticStorage.UpdateIfStateID when the
// stored State.StateID doesn't match the caller's expected value. The
// caller should reload, re-apply, and retry.
var ErrConflict = errors.New("optimistic concurrency conflict")

// OptimisticStorage is implemented by backends that support
// compare-and-swap Updates keyed on State.StateID. When the Manager
// detects this interface AND a non-default match.SchemaVersion mode is
// enabled, it switches Move from "hold the per-match write lock" to
// "race on shared storage with retry on ErrConflict" — the foundation
// for running multiple Manager instances against the same database
// without sticky-session load balancing.
//
// Adapters that store the full Match as a single JSON blob can satisfy
// this by extracting state_id into a real column and tightening the
// UPDATE: WHERE id = ? AND state_id = ?.
type OptimisticStorage interface {
	Storage
	// UpdateIfStateID writes m only if the persisted row's
	// State.StateID equals expectedStateID. Returns ErrConflict on
	// mismatch and any other backend error verbatim.
	UpdateIfStateID(m *Match, expectedStateID int) error
}
