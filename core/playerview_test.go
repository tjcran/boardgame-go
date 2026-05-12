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
