package modulebridge

import (
	"testing"

	"github.com/tjcran/boardgame-go/modules/ccg"
)

func TestEntityToken_RoundTrips(t *testing.T) {
	tok := EntityToken(ccg.EntityID(7))
	if tok != "ent:7" {
		t.Fatalf("got %q, want ent:7", tok)
	}
	id, err := ParseEntityToken("ent:7")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if id != ccg.EntityID(7) {
		t.Fatalf("got %d, want 7", id)
	}
}

func TestParseEntityToken_RejectsGarbage(t *testing.T) {
	for _, bad := range []string{"7", "ent:", "ent:x", "hex:1,2", ""} {
		if _, err := ParseEntityToken(bad); err == nil {
			t.Fatalf("expected error for %q", bad)
		}
	}
}
