package llm

import (
	"context"
	"encoding/json"
)

// Provider is the engine's abstraction over chat-completion APIs.
// Concrete implementations: OpenAIProvider (which also serves OpenRouter,
// Together, Groq, etc. via BaseURL override) and AnthropicProvider.
// Tests use MockProvider.
type Provider interface {
	// Chat issues one request and returns the model's reply.
	// Implementations must honour ctx cancellation.
	Chat(ctx context.Context, req ChatRequest) (ChatResponse, error)
}

// Role of a Message in the conversation.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// Message is one turn in a chat conversation. For tool-use turns:
//
//   - Role=assistant + ToolCalls populated: the LLM is calling tools.
//   - Role=tool + ToolCallID + Content: the caller's reply to a tool
//     call (the result of running the tool). Used for multi-step
//     agentic flows; LLMBot doesn't currently produce these.
type Message struct {
	Role       Role       `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"toolCalls,omitempty"`
	ToolCallID string     `json:"toolCallId,omitempty"`
}

// Tool describes a function the model may call. Parameters is a JSON
// Schema object; the engine produces it from bots.Action via the
// tools.go helpers.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
}

// ToolCall is one tool invocation requested by the model.
type ToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// ToolChoice tells the model how to use tools.
type ToolChoice string

const (
	// ToolChoiceAuto lets the model pick whether to call a tool.
	ToolChoiceAuto ToolChoice = "auto"
	// ToolChoiceRequired forces the model to call a tool.
	ToolChoiceRequired ToolChoice = "required"
	// ToolChoiceNone disallows tool calls (free-text only).
	ToolChoiceNone ToolChoice = "none"
)

// ChatRequest is the provider-agnostic shape. Concrete providers
// translate to/from their wire format.
type ChatRequest struct {
	Model       string
	System      string
	Messages    []Message
	Tools       []Tool
	ToolChoice  ToolChoice
	Temperature float64
	MaxTokens   int
}

// ChatResponse is the model's reply. Exactly one of Content or
// ToolCalls is typically populated, but both can co-occur (the model
// may emit prose alongside a tool call).
type ChatResponse struct {
	Content      string
	ToolCalls    []ToolCall
	FinishReason string
	// Usage is the provider's reported token count. Optional; not
	// every provider returns it on every endpoint.
	Usage Usage
}

// Usage is the post-call token count, when available.
type Usage struct {
	InputTokens  int
	OutputTokens int
}

// ProviderError is returned when a provider call fails. Wraps the
// underlying error so callers can errors.As / errors.Is against it.
type ProviderError struct {
	Provider string
	Status   int
	Message  string
	Err      error
}

func (e *ProviderError) Error() string {
	if e.Status > 0 {
		return e.Provider + " HTTP " + httpStatus(e.Status) + ": " + e.Message
	}
	return e.Provider + ": " + e.Message
}

func (e *ProviderError) Unwrap() error { return e.Err }

// httpStatus is a no-deps formatter that avoids pulling fmt for one
// integer. Stays inline-able.
func httpStatus(s int) string {
	const digits = "0123456789"
	if s == 0 {
		return "0"
	}
	var buf [4]byte
	i := 4
	for s > 0 {
		i--
		buf[i] = digits[s%10]
		s /= 10
	}
	return string(buf[i:])
}
