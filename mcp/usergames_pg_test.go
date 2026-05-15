package mcp

import (
	"testing"
)

func TestPostgresUserGames_Suite(t *testing.T) {
	dsn := pgDSN(t) // skip-if-empty helper already exists in ownership_pg_test.go
	s, err := OpenPostgresUserGames(dsn)
	if err != nil {
		t.Fatalf("OpenPostgresUserGames: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if _, err := s.db.Exec(`TRUNCATE TABLE user_games`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	runUserGameStoreSuite(t, s)
}
