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

// TestPlayerViewRedactsBlockPayload guards against the hidden-information
// leak where a pending block's Data/Target — the manual-target prompt
// contents (candidate lists, source IDs, generated names) — rode along to
// opposing seats and spectators through PlayerView. The payload must be
// visible only to the seat the block is addressed to; every other viewer
// sees the ownership shell (Tag / PlayerID) but no payload.
func TestPlayerViewRedactsBlockPayload(t *testing.T) {
	game := &Game{Name: "block-redact-test"}
	state := State{
		Blocks: []BlockSpec{
			{
				Tag:      "pickTarget",
				PlayerID: "0",
				Data:     map[string]any{"sourceCard": "ace-of-spades"},
				Target: &TargetRequest{
					Kind:       "pickTarget",
					Candidates: []any{"card-7", "card-9"},
					Source:     "ace-of-spades",
					Data:       map[string]any{"hint": "opponent's face-down"},
				},
			},
		},
	}

	// The owning seat sees the full prompt.
	viewSelf := PlayerView(game, state, "0")
	if viewSelf.Blocks[0].Data == nil || viewSelf.Blocks[0].Target == nil {
		t.Fatalf("owning seat should see its own block payload; got %+v", viewSelf.Blocks[0])
	}
	if len(viewSelf.Blocks[0].Target.Candidates) != 2 {
		t.Fatalf("owning seat should see candidates; got %+v", viewSelf.Blocks[0].Target)
	}

	// The opposing seat sees the shell but no payload.
	viewOther := PlayerView(game, state, "1")
	if got := viewOther.Blocks[0]; got.Tag != "pickTarget" || got.PlayerID != "0" {
		t.Fatalf("ownership shell should survive redaction; got %+v", got)
	}
	if viewOther.Blocks[0].Data != nil {
		t.Fatalf("block Data leaked to opposing seat: %v", viewOther.Blocks[0].Data)
	}
	if viewOther.Blocks[0].Target != nil {
		t.Fatalf("block Target leaked to opposing seat: %+v", viewOther.Blocks[0].Target)
	}

	// Spectators (playerID "") match no seat, so they see no payload.
	viewSpec := PlayerView(game, state, "")
	if viewSpec.Blocks[0].Data != nil || viewSpec.Blocks[0].Target != nil {
		t.Fatalf("block payload leaked to spectator: %+v", viewSpec.Blocks[0])
	}

	// Redaction must not mutate the source blocks — the authoritative
	// state must still carry the payload for the reducer / resume move.
	if state.Blocks[0].Data == nil || state.Blocks[0].Target == nil {
		t.Fatalf("PlayerView mutated source block payload: %+v", state.Blocks[0])
	}
}
