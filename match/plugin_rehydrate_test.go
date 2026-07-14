package match

import (
	"testing"

	"github.com/tjcran/boardgame-go/games/tictactoe"
	"github.com/tjcran/boardgame-go/plugins/random"
	"github.com/tjcran/boardgame-go/storage"
)

// TestFlatFileRoundTripRehydratesPluginData is the plugin analogue of
// issue #80 (TestFlatFileRoundTripPreservesTypedG): a serializing store
// decodes State.Plugins values as map[string]any, and a plugin whose API
// hook type-asserts a concrete type (random's *state) panics on the first
// move after a reload. The manager's PluginDecode pass must restore the
// typed value.
func TestFlatFileRoundTripRehydratesPluginData(t *testing.T) {
	dir := t.TempDir()
	flat, err := storage.NewFlatFile(dir)
	if err != nil {
		t.Fatalf("flatfile: %v", err)
	}
	m := NewManager(flat)
	game := tictactoe.New()
	game.Plugins = append(game.Plugins, random.New(uint64(42)))
	m.MustRegister(game)

	id, err := m.Create("tic-tac-toe", CreateOptions{})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	alice, err := m.Join(id, "alice", JoinOptions{})
	if err != nil {
		t.Fatalf("join alice: %v", err)
	}
	if _, err := m.Join(id, "bob", JoinOptions{}); err != nil {
		t.Fatalf("join bob: %v", err)
	}

	// Reload from the serializing store: plugin data must come back
	// typed, not as the raw JSON map.
	loaded, err := m.State(id)
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	if _, isMap := loaded.State.Plugins[random.PluginName].(map[string]any); isMap {
		t.Fatalf("plugin data still map[string]any after reload — PluginDecode pass did not run")
	}

	// The first move after the reload exercises buildPluginAPIs, which
	// type-asserts the plugin data — the exact panic site pre-fix.
	if _, err := m.Move(id, alice.PlayerID, alice.PlayerCredentials, "clickCell", []any{4}); err != nil {
		t.Fatalf("move after reload: %v", err)
	}
}
