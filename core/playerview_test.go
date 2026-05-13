package core

import (
	"reflect"
	"testing"
)

func TestStripSecretsRemovesSecretKey(t *testing.T) {
	g := map[string]any{
		"deck":   42,
		"secret": map[string]any{"answer": 42},
	}
	out := StripSecrets(g, Ctx{}, "0").(map[string]any)
	if _, ok := out["secret"]; ok {
		t.Fatal("expected secret key removed")
	}
	if out["deck"] != 42 {
		t.Fatalf("non-secret keys preserved: %+v", out)
	}
}

func TestStripSecretsRedactsOtherPlayers(t *testing.T) {
	g := map[string]any{
		"players": map[string]any{
			"0": map[string]any{"hand": []int{1, 2}},
			"1": map[string]any{"hand": []int{3, 4}},
		},
	}
	for _, pid := range []string{"0", "1"} {
		out := StripSecrets(g, Ctx{}, pid).(map[string]any)
		players := out["players"].(map[string]any)
		if len(players) != 1 || players[pid] == nil {
			t.Fatalf("expected only own player visible, got %+v", players)
		}
	}
	// Spectators see no per-player entries.
	out := StripSecrets(g, Ctx{}, "").(map[string]any)
	if len(out["players"].(map[string]any)) != 0 {
		t.Fatalf("spectator should see empty players, got %+v", out["players"])
	}
}

func TestStripSecretsPassesThroughNonMapG(t *testing.T) {
	type s struct{ X int }
	in := &s{X: 1}
	out := StripSecrets(in, Ctx{}, "0")
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("non-map G should be passed through")
	}
}

// TestPlayerViewRedactsLogArgs guards against a regression where
// LogEntry.Args slipped through PlayerView for entries marked
// Move.Redact: true. The args must be visible to the originating
// player and nilified for everyone else (including spectators).
func TestPlayerViewRedactsLogArgs(t *testing.T) {
	game := &Game{Name: "redact-test"}
	state := State{
		Log: []LogEntry{
			{Kind: "move", Move: "peek", PlayerID: "0", Args: []any{"secret"}, Redact: true},
			{Kind: "move", Move: "open", PlayerID: "1", Args: []any{"public"}, Redact: false},
		},
		Undone: []LogEntry{
			{Kind: "move", Move: "peek", PlayerID: "0", Args: []any{"undone-secret"}, Redact: true},
		},
	}

	viewSelf := PlayerView(game, state, "0")
	if got := viewSelf.Log[0].Args; len(got) != 1 || got[0] != "secret" {
		t.Fatalf("own redacted args should be visible to originator; got %v", got)
	}
	if got := viewSelf.Undone[0].Args; len(got) != 1 || got[0] != "undone-secret" {
		t.Fatalf("own redacted undone args should be visible to originator; got %v", got)
	}

	viewOther := PlayerView(game, state, "1")
	if viewOther.Log[0].Args != nil {
		t.Fatalf("redacted args leaked to non-originating seat: %v", viewOther.Log[0].Args)
	}
	if got := viewOther.Log[1].Args; len(got) != 1 || got[0] != "public" {
		t.Fatalf("non-redacted args should be visible: got %v", got)
	}
	if viewOther.Undone[0].Args != nil {
		t.Fatalf("redacted undone args leaked: %v", viewOther.Undone[0].Args)
	}

	viewSpec := PlayerView(game, state, "")
	if viewSpec.Log[0].Args != nil {
		t.Fatalf("redacted args leaked to spectator: %v", viewSpec.Log[0].Args)
	}

	// The redaction must not mutate the source slice — subsequent
	// views of the same state must still observe the original Args.
	if len(state.Log[0].Args) != 1 || state.Log[0].Args[0] != "secret" {
		t.Fatalf("PlayerView mutated source log args: %v", state.Log[0].Args)
	}
}
