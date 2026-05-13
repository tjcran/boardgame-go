package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"
)

// AnthropicOpts configures NewAnthropicProvider. APIVersion defaults to
// the latest stable version Anthropic accepts at the time of writing
// ("2023-06-01"); override if you need a different revision.
type AnthropicOpts struct {
	APIKey       string
	BaseURL      string // default "https://api.anthropic.com"
	APIVersion   string // default "2023-06-01"
	HTTPClient   *http.Client
	ExtraHeaders map[string]string
}

// AnthropicProvider implements Provider over Anthropic's Messages API.
// Wire format differs from OpenAI: `system` is a top-level field, the
// response is a content array (text blocks + tool_use blocks), tools
// use `input_schema` rather than `parameters`.
type AnthropicProvider struct {
	opts AnthropicOpts
	hc   *http.Client
	url  string
}

// NewAnthropicProvider wires an Anthropic provider.
func NewAnthropicProvider(opts AnthropicOpts) *AnthropicProvider {
	if opts.BaseURL == "" {
		opts.BaseURL = "https://api.anthropic.com"
	}
	if opts.APIVersion == "" {
		opts.APIVersion = "2023-06-01"
	}
	hc := opts.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 60 * time.Second}
	}
	return &AnthropicProvider{
		opts: opts,
		hc:   hc,
		url:  opts.BaseURL + "/v1/messages",
	}
}

// Chat implements Provider.
func (p *AnthropicProvider) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	wire := anthropicRequest{
		Model:       req.Model,
		System:      req.System,
		Messages:    convertMessagesToAnthropic(req.Messages),
		MaxTokens:   nonZero(req.MaxTokens, 1024),
		Temperature: req.Temperature,
	}
	if len(req.Tools) > 0 {
		wire.Tools = make([]anthropicTool, len(req.Tools))
		for i, t := range req.Tools {
			wire.Tools[i] = anthropicTool{
				Name:        t.Name,
				Description: t.Description,
				InputSchema: t.Parameters,
			}
		}
		switch req.ToolChoice {
		case ToolChoiceRequired:
			wire.ToolChoice = json.RawMessage(`{"type":"any"}`)
		case ToolChoiceNone:
			// Anthropic doesn't have a "no tools" mode separate from
			// "no tools supplied" — if the caller wanted to disallow
			// tool use, they should send tools=[]. We mirror OpenAI's
			// API by quietly dropping tool_choice.
		default:
			wire.ToolChoice = json.RawMessage(`{"type":"auto"}`)
		}
	}

	body, err := json.Marshal(wire)
	if err != nil {
		return ChatResponse{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.url, bytes.NewReader(body))
	if err != nil {
		return ChatResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.opts.APIKey)
	httpReq.Header.Set("anthropic-version", p.opts.APIVersion)
	for k, v := range p.opts.ExtraHeaders {
		httpReq.Header.Set(k, v)
	}

	resp, err := p.hc.Do(httpReq)
	if err != nil {
		return ChatResponse{}, &ProviderError{Provider: "anthropic", Err: err, Message: err.Error()}
	}
	defer resp.Body.Close()

	rawBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return ChatResponse{}, &ProviderError{
			Provider: "anthropic", Status: resp.StatusCode,
			Message: string(rawBody),
		}
	}

	var out anthropicResponse
	if err := json.Unmarshal(rawBody, &out); err != nil {
		return ChatResponse{}, &ProviderError{Provider: "anthropic", Message: "decode: " + err.Error(), Err: err}
	}
	if len(out.Content) == 0 {
		return ChatResponse{}, errors.New("anthropic: empty content")
	}
	r := ChatResponse{
		FinishReason: out.StopReason,
		Usage: Usage{
			InputTokens:  out.Usage.InputTokens,
			OutputTokens: out.Usage.OutputTokens,
		},
	}
	// Content is an array of blocks: {type: "text", text} or
	// {type: "tool_use", id, name, input}. Concatenate text blocks
	// and harvest tool calls.
	for _, block := range out.Content {
		switch block.Type {
		case "text":
			r.Content += block.Text
		case "tool_use":
			r.ToolCalls = append(r.ToolCalls, ToolCall{
				ID:        block.ID,
				Name:      block.Name,
				Arguments: block.Input,
			})
		}
	}
	return r, nil
}

// --- Anthropic wire types ---------------------------------------------------

type anthropicRequest struct {
	Model       string             `json:"model"`
	System      string             `json:"system,omitempty"`
	Messages    []anthropicMessage `json:"messages"`
	Tools       []anthropicTool    `json:"tools,omitempty"`
	ToolChoice  json.RawMessage    `json:"tool_choice,omitempty"`
	MaxTokens   int                `json:"max_tokens"`
	Temperature float64            `json:"temperature,omitempty"`
}

type anthropicMessage struct {
	Role    string             `json:"role"`
	Content []anthropicBlockIn `json:"content"`
}

// anthropicBlockIn is the request-side content block. Anthropic accepts
// either a string (auto-typed as text) or an array of typed blocks.
// We always send the array form for consistency.
type anthropicBlockIn struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   string          `json:"content,omitempty"` // tool_result string
	ID        string          `json:"id,omitempty"`      // tool_use
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
}

type anthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type anthropicResponse struct {
	Content []struct {
		Type  string          `json:"type"`
		Text  string          `json:"text,omitempty"`
		ID    string          `json:"id,omitempty"`
		Name  string          `json:"name,omitempty"`
		Input json.RawMessage `json:"input,omitempty"`
	} `json:"content"`
	StopReason string `json:"stop_reason"`
	Usage      struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// convertMessagesToAnthropic flattens our Role-based shape into
// Anthropic's user/assistant convention. System is sent as a top-level
// field, not as a Message.
func convertMessagesToAnthropic(msgs []Message) []anthropicMessage {
	out := make([]anthropicMessage, 0, len(msgs))
	for _, m := range msgs {
		if m.Role == RoleSystem {
			continue // system goes top-level in the request struct
		}
		blocks := []anthropicBlockIn{}
		if m.Content != "" {
			blocks = append(blocks, anthropicBlockIn{Type: "text", Text: m.Content})
		}
		for _, tc := range m.ToolCalls {
			blocks = append(blocks, anthropicBlockIn{
				Type: "tool_use", ID: tc.ID, Name: tc.Name, Input: tc.Arguments,
			})
		}
		if m.Role == RoleTool {
			blocks = append(blocks, anthropicBlockIn{
				Type: "tool_result", ToolUseID: m.ToolCallID, Content: m.Content,
			})
		}
		role := string(m.Role)
		if m.Role == RoleTool {
			// Anthropic puts tool_result blocks inside a user message.
			role = "user"
		}
		out = append(out, anthropicMessage{Role: role, Content: blocks})
	}
	return out
}

func nonZero(v, def int) int {
	if v == 0 {
		return def
	}
	return v
}
