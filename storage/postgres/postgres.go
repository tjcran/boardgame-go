// Package postgres is a Postgres-backed storage adapter for boardgame-go.
// Useful for production deployments where you want durable matches and
// you're running on Postgres anyway.
//
// Schema is a single table; the full Match is stored as a JSONB blob in
// `payload`. JSONB lets you index/query into the JSON server-side if
// later needs warrant, while keeping the Go-side code trivial.
//
//	CREATE TABLE matches (
//	    id          TEXT PRIMARY KEY,
//	    game_name   TEXT NOT NULL,
//	    payload     JSONB NOT NULL,
//	    created_at  BIGINT NOT NULL
//	);
//	CREATE INDEX matches_game_name ON matches(game_name);
//
// Uses pgx via the database/sql driver so the surface mirrors the
// SQLite adapter line-for-line. For huge fleets / sub-millisecond
// requirements, dropping to pgx's native API would shave allocations
// — out of scope here.
package postgres

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"

	_ "github.com/jackc/pgx/v5/stdlib" // pgx as a database/sql driver

	"github.com/tjcran/boardgame-go/storage"
)

// Storage is the Postgres implementation of storage.Storage.
type Storage struct {
	db *sql.DB
	mu sync.Mutex // serialises writes locally; Postgres handles the rest
}

// Open connects to Postgres at the given DSN and ensures the schema is
// present. DSN examples: "postgres://user:pass@host:5432/dbname",
// or a key=value pair string accepted by pgx.
func Open(dsn string) (*Storage, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("postgres open: %w", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("postgres ping: %w", err)
	}
	s := &Storage{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close releases the underlying connection pool.
func (s *Storage) Close() error { return s.db.Close() }

func (s *Storage) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS matches (
			id         TEXT PRIMARY KEY,
			game_name  TEXT NOT NULL,
			payload    JSONB NOT NULL,
			created_at BIGINT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS matches_game_name ON matches(game_name)`,
	}
	for _, q := range stmts {
		if _, err := s.db.Exec(q); err != nil {
			return fmt.Errorf("postgres migrate: %w", err)
		}
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
		`INSERT INTO matches (id, game_name, payload, created_at) VALUES ($1, $2, $3, $4)`,
		m.ID, m.GameName, string(payload), m.CreatedAt,
	)
	return err
}

func (s *Storage) Get(id string) (*storage.Match, error) {
	var payload string
	err := s.db.QueryRow(`SELECT payload FROM matches WHERE id = $1`, id).Scan(&payload)
	if err == sql.ErrNoRows {
		return nil, storage.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var m storage.Match
	if err := json.Unmarshal([]byte(payload), &m); err != nil {
		return nil, fmt.Errorf("postgres decode %s: %w", id, err)
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
	res, err := s.db.Exec(
		`UPDATE matches SET payload = $1, game_name = $2 WHERE id = $3`,
		string(payload), m.GameName, m.ID,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return storage.ErrNotFound
	}
	return nil
}

// UpdateIfStateID implements storage.OptimisticStorage. Uses Postgres
// JSONB extraction to read the persisted state_id and CAS in one
// statement.
//
// Falls back to non-OCC behaviour when the stored payload has no
// _stateID (older records): the WHERE clause matches anyway.
func (s *Storage) UpdateIfStateID(m *storage.Match, expectedStateID int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	payload, err := json.Marshal(m)
	if err != nil {
		return err
	}
	res, err := s.db.Exec(
		`UPDATE matches
		   SET payload = $1, game_name = $2
		 WHERE id = $3
		   AND COALESCE((payload->'state'->>'_stateID')::int, 0) = $4`,
		string(payload), m.GameName, m.ID, expectedStateID,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		// Was it absent, or just a stateID mismatch? Distinguish for
		// the caller — ErrConflict says "retry", ErrNotFound says
		// "give up".
		var dummy string
		err := s.db.QueryRow(`SELECT id FROM matches WHERE id = $1`, m.ID).Scan(&dummy)
		if err == sql.ErrNoRows {
			return storage.ErrNotFound
		}
		return storage.ErrConflict
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
		rows, err = s.db.Query(`SELECT payload FROM matches WHERE game_name = $1`, gameName)
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
	res, err := s.db.Exec(`DELETE FROM matches WHERE id = $1`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return storage.ErrNotFound
	}
	return nil
}
