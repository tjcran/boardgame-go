package core

import (
	"encoding/json"
	"testing"
)

// TestCtx_ActivePlayersRoundTripPreservesNil reproduces the bug where a Ctx
// with nil ActivePlayers marshals to `activePlayers: {}` (BGIO compat) but
// unmarshals back to a non-nil empty map — which authorisation logic then
// interprets as "stages are gated; no one is in the map; reject every move".
// Surfaces every time state is persisted (SQLite, Postgres).
func TestCtx_ActivePlayersRoundTripPreservesNil(t *testing.T) {
	in := Ctx{NumPlayers: 2, CurrentPlayer: "0", Turn: 1}
	if in.ActivePlayers != nil {
		t.Fatalf("test fixture: ActivePlayers should start nil")
	}

	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var out Ctx
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.ActivePlayers != nil {
		t.Fatalf("ActivePlayers should be nil after roundtrip, got %#v (len=%d)",
			out.ActivePlayers, len(out.ActivePlayers))
	}
}

// TestCtx_ActivePlayersRoundTripPreservesNonEmpty ensures real stage maps
// survive the roundtrip intact — the fix only normalises empty back to nil,
// not anything more.
func TestCtx_ActivePlayersRoundTripPreservesNonEmpty(t *testing.T) {
	in := Ctx{
		NumPlayers:    3,
		CurrentPlayer: "0",
		ActivePlayers: map[string]string{"1": "discard", "2": "discard"},
	}
	raw, _ := json.Marshal(in)
	var out Ctx
	_ = json.Unmarshal(raw, &out)
	if out.ActivePlayers["1"] != "discard" || out.ActivePlayers["2"] != "discard" {
		t.Errorf("ActivePlayers lost on roundtrip: %#v", out.ActivePlayers)
	}
	if _, ok := out.ActivePlayers["0"]; ok {
		t.Errorf("player 0 should not be in ActivePlayers")
	}
}
