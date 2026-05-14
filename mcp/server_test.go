package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/tjcran/boardgame-go/games/tictactoe"
	"github.com/tjcran/boardgame-go/match"
	"github.com/tjcran/boardgame-go/storage"
)

// serverHarness wires a freshly-registered Server backed by a real
// match.Manager with tic-tac-toe, ready to receive JSON-RPC frames.
func serverHarness(t *testing.T) (*Server, *Tools) {
	t.Helper()
	mgr := match.NewManager(storage.NewMemory())
	if err := mgr.Register(tictactoe.New()); err != nil {
		t.Fatalf("register tic-tac-toe: %v", err)
	}
	tools := &Tools{Manager: mgr}
	srv := NewServer(ServerInfo{Name: "test", Version: "0.0.1"}, "test instructions")
	RegisterTools(srv, tools)
	return srv, tools
}

// run pipes the given JSON-RPC lines through ServeStdio and returns the
// decoded responses, one per request that has a non-null id.
func run(t *testing.T, srv *Server, requests ...any) []rpcResponse {
	t.Helper()
	var in bytes.Buffer
	enc := json.NewEncoder(&in)
	for _, r := range requests {
		if err := enc.Encode(r); err != nil {
			t.Fatalf("encode request: %v", err)
		}
	}
	var out, errlog bytes.Buffer
	if err := srv.ServeStdio(context.Background(), &in, &out, &errlog); err != nil {
		t.Fatalf("ServeStdio: %v (stderr: %s)", err, errlog.String())
	}
	// Decode each line of out as one rpcResponse.
	var responses []rpcResponse
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if line == "" {
			continue
		}
		var resp rpcResponse
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			t.Fatalf("parse response %q: %v", line, err)
		}
		responses = append(responses, resp)
	}
	return responses
}

func TestInitializeHandshake(t *testing.T) {
	srv, _ := serverHarness(t)
	resps := run(t, srv, map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{"protocolVersion": ProtocolVersion},
	})
	if len(resps) != 1 {
		t.Fatalf("want 1 response, got %d", len(resps))
	}
	if resps[0].Error != nil {
		t.Fatalf("initialize errored: %+v", resps[0].Error)
	}
	result, _ := resps[0].Result.(map[string]any)
	if result["protocolVersion"] != ProtocolVersion {
		t.Errorf("protocolVersion = %v, want %s", result["protocolVersion"], ProtocolVersion)
	}
	if _, ok := result["serverInfo"]; !ok {
		t.Error("missing serverInfo in initialize response")
	}
	if _, ok := result["instructions"]; !ok {
		t.Error("missing instructions in initialize response")
	}
}

func TestToolsListReturnsAllSix(t *testing.T) {
	srv, _ := serverHarness(t)
	resps := run(t, srv, map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/list",
	})
	if len(resps) != 1 || resps[0].Error != nil {
		t.Fatalf("unexpected response: %+v", resps)
	}
	result := resps[0].Result.(map[string]any)
	tools := result["tools"].([]any)
	want := map[string]bool{
		"list_games": true, "create_match": true, "join_match": true,
		"get_state": true, "list_legal_moves": true, "make_move": true,
	}
	for _, tIface := range tools {
		tMap := tIface.(map[string]any)
		name := tMap["name"].(string)
		delete(want, name)
		// Every tool must have an inputSchema.
		if _, ok := tMap["inputSchema"]; !ok {
			t.Errorf("tool %q missing inputSchema", name)
		}
	}
	if len(want) > 0 {
		t.Errorf("missing tools in tools/list: %v", want)
	}
}

func TestToolsCallEndToEnd(t *testing.T) {
	srv, _ := serverHarness(t)

	// list_games via JSON-RPC.
	resps := run(t, srv, map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": "list_games", "arguments": map[string]any{}},
	})
	if len(resps) != 1 || resps[0].Error != nil {
		t.Fatalf("list_games rpc failed: %+v", resps)
	}
	result := resps[0].Result.(map[string]any)
	if got, _ := result["isError"].(bool); got {
		t.Fatalf("list_games returned isError=true: %v", result)
	}
	content := result["content"].([]any)
	textPayload := content[0].(map[string]any)["text"].(string)
	if !strings.Contains(textPayload, "tic-tac-toe") {
		t.Errorf("list_games payload missing tic-tac-toe: %s", textPayload)
	}
}

func TestToolErrorSurfacesAsIsErrorContent(t *testing.T) {
	srv, _ := serverHarness(t)
	// create_match with no game name — should produce an isError content,
	// not a JSON-RPC error.
	resps := run(t, srv, map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": "create_match", "arguments": map[string]any{}},
	})
	if len(resps) != 1 || resps[0].Error != nil {
		t.Fatalf("unexpected JSON-RPC error: %+v", resps)
	}
	result := resps[0].Result.(map[string]any)
	if got, _ := result["isError"].(bool); !got {
		t.Errorf("expected isError=true, got %v", result)
	}
}

func TestUnknownMethodReturnsRPCError(t *testing.T) {
	srv, _ := serverHarness(t)
	resps := run(t, srv, map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "no/such/method",
	})
	if len(resps) != 1 {
		t.Fatalf("want 1 response, got %d", len(resps))
	}
	if resps[0].Error == nil {
		t.Fatal("expected JSON-RPC error for unknown method")
	}
	if resps[0].Error.Code != codeMethodNotFound {
		t.Errorf("error code = %d, want %d", resps[0].Error.Code, codeMethodNotFound)
	}
}

func TestNotificationSkipsResponse(t *testing.T) {
	srv, _ := serverHarness(t)
	// notifications/initialized has no id and expects no response.
	resps := run(t, srv, map[string]any{
		"jsonrpc": "2.0", "method": "notifications/initialized",
	})
	if len(resps) != 0 {
		t.Fatalf("notification should not produce a response, got %+v", resps)
	}
}
