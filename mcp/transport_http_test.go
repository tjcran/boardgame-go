package mcp

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// httpCall sends one JSON-RPC message to the test server and returns the
// HTTP status + decoded body (or empty rpcResponse for empty bodies).
func httpCall(t *testing.T, ts *httptest.Server, msg any) (int, rpcResponse) {
	t.Helper()
	body, _ := json.Marshal(msg)
	resp, err := http.Post(ts.URL, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if len(raw) == 0 {
		return resp.StatusCode, rpcResponse{}
	}
	var parsed rpcResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("decode response: %v (raw=%s)", err, raw)
	}
	return resp.StatusCode, parsed
}

func newHTTPHarness(t *testing.T) *httptest.Server {
	t.Helper()
	srv, _ := serverHarness(t)
	RegisterDefaultPrompts(srv)
	return httptest.NewServer(srv.HTTPHandler())
}

func TestHTTPInitialize(t *testing.T) {
	ts := newHTTPHarness(t)
	defer ts.Close()

	status, resp := httpCall(t, ts, map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{"protocolVersion": ProtocolVersion},
	})
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	result := resp.Result.(map[string]any)
	if result["protocolVersion"] != ProtocolVersion {
		t.Errorf("protocolVersion mismatch: %v", result["protocolVersion"])
	}
}

func TestHTTPToolsCallEndToEnd(t *testing.T) {
	ts := newHTTPHarness(t)
	defer ts.Close()

	status, resp := httpCall(t, ts, map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": "list_games", "arguments": map[string]any{}},
	})
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	if resp.Error != nil {
		t.Fatalf("rpc error: %+v", resp.Error)
	}
	content := resp.Result.(map[string]any)["content"].([]any)
	text := content[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "tic-tac-toe") {
		t.Errorf("payload missing tic-tac-toe: %s", text)
	}
}

func TestHTTPNotificationReturns202(t *testing.T) {
	ts := newHTTPHarness(t)
	defer ts.Close()
	status, resp := httpCall(t, ts, map[string]any{
		"jsonrpc": "2.0", "method": "notifications/initialized",
	})
	if status != http.StatusAccepted {
		t.Errorf("status = %d, want 202", status)
	}
	if resp.JSONRPC != "" || resp.Error != nil {
		t.Errorf("notification should have empty body, got %+v", resp)
	}
}

func TestHTTPRejectsGET(t *testing.T) {
	ts := newHTTPHarness(t)
	defer ts.Close()
	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", resp.StatusCode)
	}
}

func TestHTTPInvalidJSONReturnsParseError(t *testing.T) {
	ts := newHTTPHarness(t)
	defer ts.Close()
	resp, err := http.Post(ts.URL, "application/json", strings.NewReader("not json"))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var parsed rpcResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if parsed.Error == nil || parsed.Error.Code != codeParseError {
		t.Errorf("expected parse error, got %+v", parsed)
	}
}

func TestHTTPEmptyBodyReturnsError(t *testing.T) {
	ts := newHTTPHarness(t)
	defer ts.Close()
	resp, err := http.Post(ts.URL, "application/json", strings.NewReader(""))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// We respond with a JSON-RPC error inside a 200 (matching the
		// rest of the protocol surface).
		t.Errorf("status = %d", resp.StatusCode)
	}
}
