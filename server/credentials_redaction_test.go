package server

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/tjcran/boardgame-go/storage"
)

// Credentials are persisted by serializing stores (storage.Player.Credentials
// is no longer json:"-"), so the no-leak guarantee for client output must come
// from the DTOs, not the struct tag. This guards that the client-facing match
// summary never contains a player's credential.
func TestMatchSummaryOmitsCredentials(t *testing.T) {
	const secret = "tok-SUPER-SECRET-должно-не-leak"
	m := &storage.Match{
		ID:       "m1",
		GameName: "g",
		Players: []storage.Player{
			{ID: "0", Name: "alice", Seat: "0", Credentials: secret},
		},
	}
	b, err := json.Marshal(toSummary(m))
	if err != nil {
		t.Fatalf("marshal summary: %v", err)
	}
	if strings.Contains(string(b), secret) {
		t.Fatalf("client match summary leaked credential: %s", b)
	}
	if strings.Contains(string(b), "credentials") {
		t.Fatalf("client match summary contains a credentials field: %s", b)
	}
}
