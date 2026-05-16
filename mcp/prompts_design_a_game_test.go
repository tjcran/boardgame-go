package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestDesignAGamePromptShape(t *testing.T) {
	s := NewServer(ServerInfo{Name: "test", Version: "0"}, "")
	RegisterDesignAGamePrompt(s)

	// Drive prompts/get directly via the server's internal lookup. The
	// public API surface is JSON-RPC; here we just need to verify the
	// prompt is registered and its body contains the expected anchors.
	s.mu.RLock()
	rp, ok := s.prompts["design-a-game"]
	s.mu.RUnlock()
	if !ok {
		t.Fatalf("design-a-game prompt not registered")
	}
	msgs, err := rp.handler(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if len(msgs) == 0 {
		t.Fatalf("no messages")
	}
	body, _ := msgs[0].Content["text"].(string)
	for _, want := range []string{
		"META",
		"setup",
		"MOVES",
		"end_if",
		"legal_moves",
		"playtest_draft",
		"register_game",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("prompt missing %q anchor", want)
		}
	}
}
