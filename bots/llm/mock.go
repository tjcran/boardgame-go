package llm

import (
	"context"
	"encoding/json"
	"errors"
)

// MockProvider is a Provider for tests. Either:
//
//   - Static: set Reply on construction; every Chat call returns it.
//   - Dynamic: set ReplyFn to compute the reply from the request.
//
// MockProvider records every received ChatRequest in Requests so
// tests can assert on what was sent.
type MockProvider struct {
	Reply    ChatResponse
	ReplyFn  func(req ChatRequest) (ChatResponse, error)
	Err      error
	Requests []ChatRequest
}

// Chat implements Provider.
func (m *MockProvider) Chat(_ context.Context, req ChatRequest) (ChatResponse, error) {
	m.Requests = append(m.Requests, req)
	if m.Err != nil {
		return ChatResponse{}, m.Err
	}
	if m.ReplyFn != nil {
		return m.ReplyFn(req)
	}
	return m.Reply, nil
}

// MockPickAction returns a ChatResponse that picks the action at the
// given index. Useful for one-liner test setup:
//
//	mock := &llm.MockProvider{ReplyFn: llm.MockPickAction(2)}
func MockPickAction(idx int) func(req ChatRequest) (ChatResponse, error) {
	return func(req ChatRequest) (ChatResponse, error) {
		if idx < 0 || idx >= len(req.Tools) {
			return ChatResponse{}, errors.New("MockPickAction: index out of range")
		}
		t := req.Tools[idx]
		return ChatResponse{
			ToolCalls: []ToolCall{
				{ID: "call_1", Name: t.Name, Arguments: json.RawMessage(`{}`)},
			},
			FinishReason: "tool_calls",
		}, nil
	}
}
