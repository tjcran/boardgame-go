package mcp

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"

	_ "github.com/jackc/pgx/v5/stdlib" // pgx as a database/sql driver
)

// PostgresOwnership is a Postgres-backed OwnershipStore for hosted
// deployments where matches must outlive a single Cloud Run instance.
//
// Schema is one table:
//
//	CREATE TABLE match_ownership (
//	    user_id    TEXT NOT NULL,
//	    match_id   TEXT NOT NULL,
//	    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
//	    PRIMARY KEY (user_id, match_id)
//	);
//	CREATE INDEX match_ownership_by_match ON match_ownership(match_id);
//
// The composite primary key makes Claim idempotent (re-claim by the
// same user is a no-op). The match_id index serves Owns lookups.
//
// We keep our own connection pool here rather than sharing one with
// storage/postgres. The price is a second pool's worth of idle
// connections — fine at hosted scale; revisit if we move to native
// pgx for both.
type PostgresOwnership struct {
	db *sql.DB
	mu sync.Mutex // serialises local writes; Postgres handles the rest
}

// OpenPostgresOwnership connects to Postgres at dsn and ensures the
// match_ownership schema is present.
func OpenPostgresOwnership(dsn string) (*PostgresOwnership, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("postgres open: %w", err)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("postgres ping: %w", err)
	}
	o := &PostgresOwnership{db: db}
	if err := o.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return o, nil
}

// Close releases the underlying connection pool.
func (o *PostgresOwnership) Close() error { return o.db.Close() }

func (o *PostgresOwnership) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS match_ownership (
			user_id    TEXT NOT NULL,
			match_id   TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			PRIMARY KEY (user_id, match_id)
		)`,
		`CREATE INDEX IF NOT EXISTS match_ownership_by_match ON match_ownership(match_id)`,
	}
	for _, q := range stmts {
		if _, err := o.db.Exec(q); err != nil {
			return fmt.Errorf("postgres ownership migrate: %w", err)
		}
	}
	return nil
}

func (o *PostgresOwnership) Claim(ctx context.Context, userID, matchID string) error {
	if userID == "" {
		return errors.New("userID required")
	}
	if matchID == "" {
		return errors.New("matchID required")
	}
	o.mu.Lock()
	defer o.mu.Unlock()

	// First, see if anyone owns it. We need to reject "user B claims a
	// match user A already owns" — that's a hostile claim attempt, not
	// the same as an idempotent re-claim by the same user.
	var existing string
	err := o.db.QueryRowContext(ctx,
		`SELECT user_id FROM match_ownership WHERE match_id = $1 LIMIT 1`,
		matchID,
	).Scan(&existing)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("check existing owner: %w", err)
	}
	if err == nil && existing != userID {
		return fmt.Errorf("match %s already owned by another user", matchID)
	}

	// Insert is idempotent because (user_id, match_id) is the PK.
	_, err = o.db.ExecContext(ctx,
		`INSERT INTO match_ownership (user_id, match_id) VALUES ($1, $2)
		 ON CONFLICT (user_id, match_id) DO NOTHING`,
		userID, matchID,
	)
	if err != nil {
		return fmt.Errorf("claim ownership: %w", err)
	}
	return nil
}

func (o *PostgresOwnership) Owns(ctx context.Context, userID, matchID string) (bool, error) {
	var exists bool
	err := o.db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM match_ownership WHERE user_id = $1 AND match_id = $2)`,
		userID, matchID,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("owns lookup: %w", err)
	}
	return exists, nil
}

func (o *PostgresOwnership) MatchesFor(ctx context.Context, userID string) ([]string, error) {
	rows, err := o.db.QueryContext(ctx,
		`SELECT match_id FROM match_ownership WHERE user_id = $1 ORDER BY created_at`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("matches-for: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var m string
		if err := rows.Scan(&m); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
