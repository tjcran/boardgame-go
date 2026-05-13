// Package sqlite is a SQLite-backed storage adapter for boardgame-go,
// useful when you want persistence across restarts without running a
// full database. Uses modernc.org/sqlite, a pure-Go SQLite — no CGO, so
// cross-compilation and small static binaries still work.
//
// Schema is one table:
//
//	matches (id PRIMARY KEY, game_name, payload JSON, created_at)
//
// The full Match (including state, log, players, plugin data) is stored
// as a single JSON blob in `payload`. Reads round-trip via encoding/json.
// This is fine for development and small fleets — high-throughput servers
// will want a row-per-update table or a different backend.
package sqlite

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"

	_ "modernc.org/sqlite" // pure-Go SQLite driver, no CGO

	"github.com/tjcran/boardgame-go/storage"
)

// Storage is the SQLite implementation of storage.Storage.
type Storage struct {
	db *sql.DB
	mu sync.Mutex // serialises writes; SQLite handles one writer at a time
}

// Open opens (or creates) a SQLite database at the given DSN and ensures
// the schema is present. Typical DSN: "file:matches.db?_pragma=journal_mode(WAL)".
func Open(dsn string) (*Storage, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("sqlite open: %w", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("sqlite ping: %w", err)
	}
	s := &Storage{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close releases the underlying database handle.
func (s *Storage) Close() error { return s.db.Close() }

func (s *Storage) migrate() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS matches (
			id         TEXT PRIMARY KEY,
			game_name  TEXT NOT NULL,
			payload    TEXT NOT NULL,
			created_at INTEGER NOT NULL
		);
		CREATE INDEX IF NOT EXISTS matches_game_name ON matches(game_name);
	`)
	if err != nil {
		return fmt.Errorf("sqlite migrate: %w", err)
	}
	return nil
}

func (s *Storage) Create(m *storage.Match) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	payload, err := json.Marshal(m)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(
		`INSERT INTO matches (id, game_name, payload, created_at) VALUES (?, ?, ?, ?)`,
		m.ID, m.GameName, string(payload), m.CreatedAt,
	)
	return err
}

func (s *Storage) Get(id string) (*storage.Match, error) {
	var payload string
	err := s.db.QueryRow(`SELECT payload FROM matches WHERE id = ?`, id).Scan(&payload)
	if err == sql.ErrNoRows {
		return nil, storage.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var m storage.Match
	if err := json.Unmarshal([]byte(payload), &m); err != nil {
		return nil, fmt.Errorf("sqlite decode %s: %w", id, err)
	}
	return &m, nil
}

func (s *Storage) Update(m *storage.Match) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	payload, err := json.Marshal(m)
	if err != nil {
		return err
	}
	res, err := s.db.Exec(`UPDATE matches SET payload = ?, game_name = ? WHERE id = ?`,
		string(payload), m.GameName, m.ID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return storage.ErrNotFound
	}
	return nil
}

func (s *Storage) List(gameName string) ([]*storage.Match, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if gameName == "" {
		rows, err = s.db.Query(`SELECT payload FROM matches`)
	} else {
		rows, err = s.db.Query(`SELECT payload FROM matches WHERE game_name = ?`, gameName)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*storage.Match
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var m storage.Match
		if err := json.Unmarshal([]byte(payload), &m); err != nil {
			continue
		}
		out = append(out, &m)
	}
	return out, rows.Err()
}

func (s *Storage) Wipe(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.Exec(`DELETE FROM matches WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return storage.ErrNotFound
	}
	return nil
}
