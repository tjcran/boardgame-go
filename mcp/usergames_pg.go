package mcp

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // pgx as a database/sql driver
)

// PostgresUserGames stores designed-game specs in the user_games table.
// Schema (created on Open):
//
//	CREATE TABLE user_games (
//	    user_id    TEXT NOT NULL,
//	    name       TEXT NOT NULL,
//	    source     TEXT NOT NULL,
//	    llm_guide  TEXT,
//	    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
//	    PRIMARY KEY (user_id, name)
//	);
//	CREATE INDEX user_games_by_user ON user_games(user_id);
type PostgresUserGames struct {
	db *sql.DB
}

// OpenPostgresUserGames connects to Postgres at dsn and ensures the
// user_games schema is present.
func OpenPostgresUserGames(dsn string) (*PostgresUserGames, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("postgres open: %w", err)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("postgres ping: %w", err)
	}
	pg := &PostgresUserGames{db: db}
	if err := pg.ensureSchema(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return pg, nil
}

// Close releases the underlying connection pool.
func (s *PostgresUserGames) Close() error { return s.db.Close() }

func (s *PostgresUserGames) ensureSchema() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS user_games (
			user_id    TEXT NOT NULL,
			name       TEXT NOT NULL,
			source     TEXT NOT NULL,
			llm_guide  TEXT,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			PRIMARY KEY (user_id, name)
		)`,
		`CREATE INDEX IF NOT EXISTS user_games_by_user ON user_games(user_id)`,
	}
	for _, q := range stmts {
		if _, err := s.db.Exec(q); err != nil {
			return fmt.Errorf("ensureSchema: %w", err)
		}
	}
	return nil
}

func (s *PostgresUserGames) Put(ctx context.Context, ug UserGame) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO user_games (user_id, name, source, llm_guide)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_id, name) DO UPDATE
		SET source = EXCLUDED.source, llm_guide = EXCLUDED.llm_guide
	`, ug.UserID, ug.Name, ug.Source, nullStr(ug.LLMGuide))
	return err
}

func (s *PostgresUserGames) Get(ctx context.Context, userID, name string) (*UserGame, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT user_id, name, source, COALESCE(llm_guide, ''), created_at
		FROM user_games WHERE user_id=$1 AND name=$2
	`, userID, name)
	var ug UserGame
	var t time.Time
	err := row.Scan(&ug.UserID, &ug.Name, &ug.Source, &ug.LLMGuide, &t)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	ug.CreatedAt = t
	return &ug, nil
}

func (s *PostgresUserGames) List(ctx context.Context, userID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT name FROM user_games WHERE user_id=$1 ORDER BY name
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (s *PostgresUserGames) ListAll(ctx context.Context) ([]UserGame, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT user_id, name, source, COALESCE(llm_guide, ''), created_at
		FROM user_games
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []UserGame{}
	for rows.Next() {
		var ug UserGame
		if err := rows.Scan(&ug.UserID, &ug.Name, &ug.Source, &ug.LLMGuide, &ug.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, ug)
	}
	return out, rows.Err()
}

func (s *PostgresUserGames) Delete(ctx context.Context, userID, name string) error {
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM user_games WHERE user_id=$1 AND name=$2
	`, userID, name)
	return err
}

// nullStr converts an empty string to nil (SQL NULL) for optional text columns.
func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}
