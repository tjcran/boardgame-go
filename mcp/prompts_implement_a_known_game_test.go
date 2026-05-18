package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestImplementAKnownGamePromptShape(t *testing.T) {
	s := NewServer(ServerInfo{Name: "t", Version: "0"}, DefaultInstructions)
	RegisterImplementAKnownGamePrompt(s)

	s.mu.RLock()
	rp, ok := s.prompts["implement-a-known-game"]
	s.mu.RUnlock()
	if !ok {
		t.Fatalf("implement-a-known-game prompt not registered")
	}
	msgs, err := rp.handler(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if len(msgs) == 0 {
		t.Fatalf("no messages")
	}
	body, _ := msgs[0].Content["text"].(string)

	// Framing — must distinguish this from the design-a-game interview.
	for _, want := range []string{
		"named a real-world board game",
		"Skip the design interview",
		"one-shot",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("prompt missing framing anchor %q", want)
		}
	}

	// Contract surface — must teach the same Starlark API.
	for _, want := range []string{
		"META",
		"setup(ctx)",
		"MOVES",
		"end_if",
		"legal_moves",
		"return the new state dict",
		"fail(",
		"playtest_draft",
		"register_game",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("prompt missing contract anchor %q", want)
		}
	}

	// Engine-limits callout — must flag the multi-action / phases gap so
	// Claude doesn't try to implement Catan silently and produce a broken
	// reduction.
	for _, want := range []string{
		"Multi-action turns",
		"phases or stages",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("prompt missing limits anchor %q", want)
		}
	}

	// Worked example anchor — sanity-check the example survived edits.
	if !strings.Contains(body, "connect-4") {
		t.Errorf("prompt missing connect-4 worked example")
	}
}
