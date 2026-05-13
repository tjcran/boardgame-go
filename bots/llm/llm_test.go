package llm_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tjcran/boardgame-go/bots"
	"github.com/tjcran/boardgame-go/bots/llm"
	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/games/tictactoe"
)

// tttEnumerate lists every empty cell as a clickCell action. Used by
// every test below.
func tttEnumerate(g core.G, _ core.Ctx, _ string) []bots.Action {
	st := g.(*tictactoe.State)
	var out []bots.Action
	for i, c := range st.Cells {
		if c == "" {
			out = append(out, bots.Action{Move: "clickCell", Args: []any{i}})
		}
	}
	return out
}

func TestLLMBotPicksFromMockProvider(t *testing.T) {
	g := tictactoe.New()
	g.Enumerate = tttEnumerate
	state := core.NewMatch(g, 0, nil)

	mock := &llm.MockProvider{ReplyFn: llm.MockPickAction(4)} // pick cell index 4

	bot := &llm.LLMBot{
		Provider: mock,
		Model:    "test-model",
		Game:     g,
		PromptFn: llm.DefaultPrompt,
	}
	action, err := bot.Play(context.Background(), state, "0")
	if err != nil {
		t.Fatalf("Play: %v", err)
	}
	if action.Move != "clickCell" || action.Args[0] != 4 {
		t.Fatalf("expected clickCell with arg 4, got %+v", action)
	}
	if len(mock.Requests) != 1 {
		t.Fatalf("expected exactly 1 chat call, got %d", len(mock.Requests))
	}
	req := mock.Requests[0]
	if len(req.Tools) != 9 {
		t.Fatalf("expected 9 tools (one per empty cell), got %d", len(req.Tools))
	}
	if req.ToolChoice != llm.ToolChoiceRequired {
		t.Fatalf("expected required tool choice, got %v", req.ToolChoice)
	}
}

func TestLLMBotRejectsUnknownTool(t *testing.T) {
	g := tictactoe.New()
	g.Enumerate = tttEnumerate
	state := core.NewMatch(g, 0, nil)

	mock := &llm.MockProvider{
		Reply: llm.ChatResponse{
			ToolCalls: []llm.ToolCall{
				{ID: "x", Name: "definitely_not_a_real_tool"},
			},
		},
	}
	bot := &llm.LLMBot{Provider: mock, Model: "m", Game: g, PromptFn: llm.DefaultPrompt}
	_, err := bot.Play(context.Background(), state, "0")
	if err == nil {
		t.Fatal("expected error on unknown tool name")
	}
}

func TestLLMBotPropagatesProviderError(t *testing.T) {
	g := tictactoe.New()
	g.Enumerate = tttEnumerate
	state := core.NewMatch(g, 0, nil)

	mock := &llm.MockProvider{Err: errors.New("rate limited")}
	bot := &llm.LLMBot{
		Provider:   mock,
		Model:      "m",
		Game:       g,
		PromptFn:   llm.DefaultPrompt,
		MaxRetries: 1,
	}
	_, err := bot.Play(context.Background(), state, "0")
	if err == nil {
		t.Fatal("expected provider error to surface")
	}
	if !strings.Contains(err.Error(), "rate limited") {
		t.Fatalf("expected error to wrap provider message, got %v", err)
	}
	// Verify retry actually happened: 1 retry = 2 attempts total.
	if len(mock.Requests) != 2 {
		t.Fatalf("expected 2 attempts (1 + 1 retry), got %d", len(mock.Requests))
	}
}

func TestFreeTextModeUsesCustomParser(t *testing.T) {
	g := tictactoe.New()
	g.Enumerate = tttEnumerate
	state := core.NewMatch(g, 0, nil)

	mock := &llm.MockProvider{
		Reply: llm.ChatResponse{Content: "I'll play cell 8", FinishReason: "stop"},
	}
	bot := &llm.LLMBot{
		Provider: mock,
		Model:    "m",
		Game:     g,
		PromptFn: llm.DefaultPrompt,
		ToolMode: llm.ToolModeFree,
		ParseFreeText: func(content string, actions []bots.Action) (bots.Action, error) {
			// Very crude parser for the test.
			for _, a := range actions {
				idx := a.Args[0].(int)
				if strings.Contains(content, "cell "+itoa(idx)) {
					return a, nil
				}
			}
			return bots.Action{}, errors.New("not found")
		},
	}
	action, err := bot.Play(context.Background(), state, "0")
	if err != nil {
		t.Fatalf("Play: %v", err)
	}
	if action.Args[0] != 8 {
		t.Fatalf("expected cell 8, got %v", action.Args[0])
	}
}

func TestOpenAIProviderWireFormat(t *testing.T) {
	var captured openAIWireRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("missing/wrong Authorization header: %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("wrong content-type: %q", got)
		}
		_ = json.NewDecoder(r.Body).Decode(&captured)
		// Echo a tool call so the bot has something to parse.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"choices": [{
				"message": {
					"role": "assistant",
					"tool_calls": [{
						"id": "c1",
						"type": "function",
						"function": {"name": "action_0_clickCell", "arguments": "{}"}
					}]
				},
				"finish_reason": "tool_calls"
			}],
			"usage": {"prompt_tokens": 10, "completion_tokens": 5}
		}`))
	}))
	defer srv.Close()

	prov := llm.NewOpenAIProvider(llm.OpenAIOpts{
		APIKey:  "test-key",
		BaseURL: srv.URL,
	})
	g := tictactoe.New()
	g.Enumerate = tttEnumerate
	state := core.NewMatch(g, 0, nil)
	bot := &llm.LLMBot{
		Provider: prov,
		Model:    "gpt-test",
		Game:     g,
		PromptFn: llm.DefaultPrompt,
	}
	action, err := bot.Play(context.Background(), state, "0")
	if err != nil {
		t.Fatalf("Play: %v", err)
	}
	if action.Move != "clickCell" {
		t.Fatalf("expected clickCell, got %+v", action)
	}
	if captured.Model != "gpt-test" {
		t.Fatalf("wrong model in wire body: %q", captured.Model)
	}
	if len(captured.Tools) == 0 {
		t.Fatal("expected tools in wire body")
	}
	if len(captured.Messages) < 1 || captured.Messages[0].Role != "system" {
		t.Fatalf("expected system message first, got %+v", captured.Messages)
	}
}

func TestAnthropicProviderWireFormat(t *testing.T) {
	var captured anthropicWireRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("x-api-key"); got != "test-key" {
			t.Errorf("missing/wrong x-api-key: %q", got)
		}
		if got := r.Header.Get("anthropic-version"); got == "" {
			t.Error("missing anthropic-version header")
		}
		_ = json.NewDecoder(r.Body).Decode(&captured)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"content": [
				{"type": "tool_use", "id": "tu1", "name": "action_0_clickCell", "input": {}}
			],
			"stop_reason": "tool_use",
			"usage": {"input_tokens": 12, "output_tokens": 4}
		}`))
	}))
	defer srv.Close()

	prov := llm.NewAnthropicProvider(llm.AnthropicOpts{
		APIKey:  "test-key",
		BaseURL: srv.URL,
	})
	g := tictactoe.New()
	g.Enumerate = tttEnumerate
	state := core.NewMatch(g, 0, nil)
	bot := &llm.LLMBot{
		Provider: prov,
		Model:    "claude-test",
		Game:     g,
		PromptFn: llm.DefaultPrompt,
	}
	action, err := bot.Play(context.Background(), state, "0")
	if err != nil {
		t.Fatalf("Play: %v", err)
	}
	if action.Move != "clickCell" {
		t.Fatalf("expected clickCell, got %+v", action)
	}
	if captured.Model != "claude-test" {
		t.Fatalf("wrong model: %q", captured.Model)
	}
	if captured.System == "" {
		t.Fatal("expected non-empty system field at top level (Anthropic wire format)")
	}
	if len(captured.Tools) == 0 {
		t.Fatal("expected tools in wire body")
	}
}

// itoa is a tiny no-deps int-to-string converter.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// Wire-format helpers for asserting against captured bodies. Shadowed
// from the package's private types so the test only sees fields it
// cares about.

type openAIWireRequest struct {
	Model    string `json:"model"`
	Messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
	Tools      []json.RawMessage `json:"tools"`
	ToolChoice json.RawMessage   `json:"tool_choice"`
}

type anthropicWireRequest struct {
	Model    string `json:"model"`
	System   string `json:"system"`
	Messages []struct {
		Role    string `json:"role"`
		Content []json.RawMessage `json:"content"`
	} `json:"messages"`
	Tools      []json.RawMessage `json:"tools"`
	ToolChoice json.RawMessage   `json:"tool_choice"`
}
