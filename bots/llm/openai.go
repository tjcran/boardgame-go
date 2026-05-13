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

// OpenAIOpts configures NewOpenAIProvider. Defaults target
// https://api.openai.com; override BaseURL for OpenRouter
// (https://openrouter.ai/api/v1), Together, Groq, Fireworks, and any
// other OpenAI-compatible endpoint.
type OpenAIOpts struct {
	APIKey  string
	BaseURL string // default "https://api.openai.com/v1"
	// HTTPClient is the underlying client. Defaults to a 60s-timeout
	// http.Client. Inject your own for retries, tracing, or VCR-style
	// recording in tests.
	HTTPClient *http.Client
	// OrgID is OpenAI's organisation header. Optional.
	OrgID string
	// ExtraHeaders are merged into every request. Use for OpenRouter's
	// Referer/X-Title attribution headers, custom tracing, etc.
	ExtraHeaders map[string]string
}

// OpenAIProvider implements Provider over the OpenAI chat-completions
// API. Also serves any OpenAI-compatible endpoint via BaseURL.
type OpenAIProvider struct {
	opts OpenAIOpts
	hc   *http.Client
	url  string
}

// NewOpenAIProvider wires an OpenAI / OpenAI-compatible provider.
func NewOpenAIProvider(opts OpenAIOpts) *OpenAIProvider {
	if opts.BaseURL == "" {
		opts.BaseURL = "https://api.openai.com/v1"
	}
	hc := opts.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 60 * time.Second}
	}
	return &OpenAIProvider{
		opts: opts,
		hc:   hc,
		url:  opts.BaseURL + "/chat/completions",
	}
}

// NewOpenRouterProvider is a convenience constructor for OpenRouter's
// endpoint. OpenRouter aliases many vendor models (Anthropic, Google,
// Meta, etc.) behind one OpenAI-shape API.
func NewOpenRouterProvider(apiKey string) *OpenAIProvider {
	return NewOpenAIProvider(OpenAIOpts{
		APIKey:  apiKey,
		BaseURL: "https://openrouter.ai/api/v1",
	})
}

// Chat implements Provider.
func (p *OpenAIProvider) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	wire := openAIRequest{
		Model:       req.Model,
		Messages:    convertMessagesToOpenAI(req.System, req.Messages),
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
	}
	if len(req.Tools) > 0 {
		wire.Tools = make([]openAITool, len(req.Tools))
		for i, t := range req.Tools {
			wire.Tools[i] = openAITool{
				Type: "function",
				Function: openAIToolFunc{
					Name:        t.Name,
					Description: t.Description,
					Parameters:  t.Parameters,
				},
			}
		}
		switch req.ToolChoice {
		case ToolChoiceRequired:
			wire.ToolChoice = json.RawMessage(`"required"`)
		case ToolChoiceNone:
			wire.ToolChoice = json.RawMessage(`"none"`)
		default:
			wire.ToolChoice = json.RawMessage(`"auto"`)
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
	httpReq.Header.Set("Authorization", "Bearer "+p.opts.APIKey)
	if p.opts.OrgID != "" {
		httpReq.Header.Set("OpenAI-Organization", p.opts.OrgID)
	}
	for k, v := range p.opts.ExtraHeaders {
		httpReq.Header.Set(k, v)
	}

	resp, err := p.hc.Do(httpReq)
	if err != nil {
		return ChatResponse{}, &ProviderError{Provider: "openai", Err: err, Message: err.Error()}
	}
	defer resp.Body.Close()

	rawBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return ChatResponse{}, &ProviderError{
			Provider: "openai", Status: resp.StatusCode,
			Message: string(rawBody),
		}
	}

	var out openAIResponse
	if err := json.Unmarshal(rawBody, &out); err != nil {
		return ChatResponse{}, &ProviderError{Provider: "openai", Message: "decode: " + err.Error(), Err: err}
	}
	if len(out.Choices) == 0 {
		return ChatResponse{}, errors.New("openai: empty choices")
	}
	msg := out.Choices[0].Message
	r := ChatResponse{
		Content:      msg.Content,
		FinishReason: out.Choices[0].FinishReason,
		Usage: Usage{
			InputTokens:  out.Usage.PromptTokens,
			OutputTokens: out.Usage.CompletionTokens,
		},
	}
	for _, tc := range msg.ToolCalls {
		r.ToolCalls = append(r.ToolCalls, ToolCall{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: tc.Function.Arguments,
		})
	}
	return r, nil
}

// --- OpenAI wire types ------------------------------------------------------

type openAIRequest struct {
	Model       string             `json:"model"`
	Messages    []openAIMessage    `json:"messages"`
	Tools       []openAITool       `json:"tools,omitempty"`
	ToolChoice  json.RawMessage    `json:"tool_choice,omitempty"`
	Temperature float64            `json:"temperature,omitempty"`
	MaxTokens   int                `json:"max_tokens,omitempty"`
}

type openAIMessage struct {
	Role       string            `json:"role"`
	Content    string            `json:"content,omitempty"`
	ToolCalls  []openAIToolCall  `json:"tool_calls,omitempty"`
	ToolCallID string            `json:"tool_call_id,omitempty"`
}

type openAITool struct {
	Type     string         `json:"type"`
	Function openAIToolFunc `json:"function"`
}

type openAIToolFunc struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
}

type openAIToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	} `json:"function"`
}

type openAIResponse struct {
	Choices []struct {
		Message      openAIMessage `json:"message"`
		FinishReason string        `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

// convertMessagesToOpenAI prepends the system message (if any) and
// translates Role values. OpenAI's tool_calls arguments are a JSON
// string in the wire format (not an object), but JSON Marshal sends
// the json.RawMessage as-is which works on both sides; OpenAI accepts
// either form.
func convertMessagesToOpenAI(system string, msgs []Message) []openAIMessage {
	out := make([]openAIMessage, 0, len(msgs)+1)
	if system != "" {
		out = append(out, openAIMessage{Role: "system", Content: system})
	}
	for _, m := range msgs {
		om := openAIMessage{
			Role:       string(m.Role),
			Content:    m.Content,
			ToolCallID: m.ToolCallID,
		}
		for _, tc := range m.ToolCalls {
			om.ToolCalls = append(om.ToolCalls, openAIToolCall{
				ID:   tc.ID,
				Type: "function",
				Function: struct {
					Name      string          `json:"name"`
					Arguments json.RawMessage `json:"arguments"`
				}{Name: tc.Name, Arguments: tc.Arguments},
			})
		}
		out = append(out, om)
	}
	return out
}
