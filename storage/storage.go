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
	ID   string `json:"id"`
	Name string `json:"name"`
	Seat string `json:"seat"` // seat index as string, matches ctx.PlayOrder
}

// Match is the persisted shape of one in-progress (or finished) game.
type Match struct {
	ID        string     `json:"id"`
	GameName  string     `json:"gameName"`
	State     core.State `json:"state"`
	Players   []Player   `json:"players"` // indexed by seat
	SetupData any        `json:"setupData,omitempty"`
	Unlisted  bool       `json:"unlisted,omitempty"`
	CreatedAt int64      `json:"createdAt"`
}

// Storage is the persistence interface the match manager depends on.
//
// All methods must be safe for concurrent use; the manager fans out HTTP and
// WebSocket requests without serialising at the call site.
type Storage interface {
	Create(m *Match) error
	Get(id string) (*Match, error)
	Update(m *Match) error
	List(gameName string) ([]*Match, error)
}
