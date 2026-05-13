package match

import (
	"fmt"

	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/storage"
)

// ExportedMatch is the portable bundle of everything needed to
// reconstruct a match: the game name, setup, and the move log. Replays
// against the registered Game produce a State byte-identical to the
// source (when the engine and game definition agree on the schema).
//
// Useful for: sharing a problematic match for debugging, seeding an
// integration test from a real production game, archiving a finished
// match in a smaller form than the raw State.
type ExportedMatch struct {
	GameName      string          `json:"gameName"`
	NumPlayers    int             `json:"numPlayers"`
	SetupData     any             `json:"setupData,omitempty"`
	Log           []core.LogEntry `json:"log"`
	SchemaVersion int             `json:"schemaVersion,omitempty"`
}

// ExportMatch returns the portable form of an existing match. Bare —
// no credentials, players list, or transport metadata; just enough to
// replay.
func (m *Manager) ExportMatch(matchID string) (*ExportedMatch, error) {
	match, err := m.loadMigrated(matchID)
	if err != nil {
		return nil, err
	}
	return &ExportedMatch{
		GameName:      match.GameName,
		NumPlayers:    match.State.Ctx.NumPlayers,
		SetupData:     match.SetupData,
		Log:           append([]core.LogEntry(nil), match.State.Log...),
		SchemaVersion: match.SchemaVersion,
	}, nil
}

// ImportMatch creates a new match by replaying the supplied log against
// the registered game. Returns the new match ID. No players are seated;
// the caller can Join afterwards.
//
// If the export's SchemaVersion is less than the game's current
// SchemaVersion, the imported state is migrated through Game.Migrate
// after replay completes.
func (m *Manager) ImportMatch(exp *ExportedMatch) (string, error) {
	g := m.Game(exp.GameName)
	if g == nil {
		return "", fmt.Errorf("%w: %s", ErrUnknownGame, exp.GameName)
	}
	state, err := core.Replay(g, exp.Log, exp.NumPlayers, exp.SetupData)
	if err != nil {
		return "", fmt.Errorf("replay: %w", err)
	}
	id := newID()
	match := &storage.Match{
		ID:            id,
		GameName:      exp.GameName,
		State:         state,
		SetupData:     exp.SetupData,
		CreatedAt:     m.now().Unix(),
		SchemaVersion: exp.SchemaVersion,
	}
	if err := m.store.Create(match); err != nil {
		return "", err
	}
	// loadMigrated handles upgrade-to-current after the Create.
	_, _ = m.loadMigrated(id)
	m.Logger.Info("match.imported",
		"match_id", id, "game", exp.GameName,
		"moves_replayed", len(exp.Log))
	return id, nil
}
