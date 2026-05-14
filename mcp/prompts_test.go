package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestPromptsListReturnsRegistered(t *testing.T) {
	srv, _ := serverHarness(t)
	RegisterDefaultPrompts(srv)

	resps := run(t, srv, map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "prompts/list",
	})
	if len(resps) != 1 || resps[0].Error != nil {
		t.Fatalf("unexpected response: %+v", resps)
	}
	prompts := resps[0].Result.(map[string]any)["prompts"].([]any)
	found := false
	for _, p := range prompts {
		if p.(map[string]any)["name"].(string) == "play-tictactoe" {
			found = true
		}
	}
	if !found {
		t.Errorf("prompts/list missing play-tictactoe: %+v", prompts)
	}
}

func TestPromptsGetReturnsMessages(t *testing.T) {
	srv, _ := serverHarness(t)
	RegisterDefaultPrompts(srv)

	resps := run(t, srv, map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "prompts/get",
		"params": map[string]any{"name": "play-tictactoe", "arguments": map[string]any{}},
	})
	if len(resps) != 1 || resps[0].Error != nil {
		t.Fatalf("unexpected response: %+v", resps)
	}
	result := resps[0].Result.(map[string]any)
	messages := result["messages"].([]any)
	if len(messages) == 0 {
		t.Fatal("expected at least one message")
	}
	first := messages[0].(map[string]any)
	if first["role"].(string) != "user" {
		t.Errorf("role = %q, want user", first["role"])
	}
	content := first["content"].(map[string]any)
	text := content["text"].(string)
	if !strings.Contains(text, "tic-tac-toe") {
		t.Errorf("prompt text doesn't mention tic-tac-toe (got %d chars)", len(text))
	}
}

func TestPromptsGetUnknownReturnsError(t *testing.T) {
	srv, _ := serverHarness(t)
	RegisterDefaultPrompts(srv)

	resps := run(t, srv, map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "prompts/get",
		"params": map[string]any{"name": "no-such-prompt"},
	})
	if len(resps) != 1 || resps[0].Error == nil {
		t.Fatalf("expected error response, got %+v", resps)
	}
	if resps[0].Error.Code != codeMethodNotFound {
		t.Errorf("error code = %d, want %d", resps[0].Error.Code, codeMethodNotFound)
	}
}

func TestInitializeAdvertisesPromptsCapability(t *testing.T) {
	srv, _ := serverHarness(t)
	RegisterDefaultPrompts(srv)

	resps := run(t, srv, map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{"protocolVersion": ProtocolVersion},
	})
	caps := resps[0].Result.(map[string]any)["capabilities"].(map[string]any)
	if _, ok := caps["prompts"]; !ok {
		t.Error("initialize result missing prompts capability")
	}
}

func TestCustomPromptHandler(t *testing.T) {
	srv, _ := serverHarness(t)
	srv.RegisterPrompt(PromptSpec{
		Name: "echo", Description: "echoes its msg arg",
		Arguments: []PromptArgument{{Name: "msg", Required: true}},
	}, func(_ context.Context, raw json.RawMessage) ([]PromptMessage, error) {
		var args struct{ Msg string }
		_ = json.Unmarshal(raw, &args)
		return []PromptMessage{textMessage("you said: " + args.Msg)}, nil
	})

	resps := run(t, srv, map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "prompts/get",
		"params": map[string]any{"name": "echo", "arguments": map[string]any{"msg": "hi"}},
	})
	messages := resps[0].Result.(map[string]any)["messages"].([]any)
	text := messages[0].(map[string]any)["content"].(map[string]any)["text"].(string)
	if text != "you said: hi" {
		t.Errorf("got %q, want %q", text, "you said: hi")
	}
}
