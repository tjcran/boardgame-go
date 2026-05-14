package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

// ProtocolVersion is the MCP wire version this server speaks. Clients
// negotiate during initialize; we accept whatever version the client sends
// and echo our supported one. 2024-11-05 is the first widely-implemented
// stable revision and is what Claude Code, Claude Desktop, and Cursor all
// understand at time of writing.
const ProtocolVersion = "2024-11-05"

// ServerInfo identifies this server to MCP clients during the initialize
// handshake. Clients display Name + Version in their UI.
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ToolSpec describes one tool exposed by the server. InputSchema is a
// JSON Schema describing the tool's arguments; clients use it to render
// argument pickers and to validate calls before invoking.
type ToolSpec struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// ToolHandler is invoked with the raw JSON arguments from a tools/call
// request. The returned value is JSON-marshalled and wrapped in the MCP
// content envelope before being sent back to the client.
type ToolHandler func(ctx context.Context, args json.RawMessage) (any, error)

// Server is a minimal MCP server. It speaks JSON-RPC 2.0 over a single
// io.Reader/io.Writer pair, which makes it transport-agnostic — stdio is
// the only mode wired up in this PR, but the same Server can be served
// over an HTTP/SSE channel later.
type Server struct {
	Info         ServerInfo
	Instructions string

	mu    sync.RWMutex
	tools map[string]registeredTool
}

type registeredTool struct {
	spec    ToolSpec
	handler ToolHandler
}

// NewServer builds an empty Server.
func NewServer(info ServerInfo, instructions string) *Server {
	return &Server{
		Info:         info,
		Instructions: instructions,
		tools:        map[string]registeredTool{},
	}
}

// RegisterTool adds one tool to the server. Replaces any existing tool of
// the same name.
func (s *Server) RegisterTool(spec ToolSpec, handler ToolHandler) {
	s.mu.Lock()
	s.tools[spec.Name] = registeredTool{spec: spec, handler: handler}
	s.mu.Unlock()
}

// ServeStdio runs the JSON-RPC loop over the given reader/writer. Each
// inbound line is parsed as a JSON-RPC 2.0 message; responses are written
// back as line-delimited JSON. Returns when the reader hits EOF or an
// unrecoverable I/O error.
//
// All protocol diagnostics go to errlog (typically os.Stderr) — never to
// the writer, which is reserved for JSON-RPC traffic.
func (s *Server) ServeStdio(ctx context.Context, r io.Reader, w io.Writer, errlog io.Writer) error {
	scanner := bufio.NewScanner(r)
	// MCP messages can be larger than the default 64KB scanner buffer
	// (e.g. a fat game state). 4MB ceiling matches what Claude Desktop's
	// stdio bridge tolerates.
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	enc := json.NewEncoder(w)
	var writeMu sync.Mutex
	writeJSON := func(v any) {
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = enc.Encode(v)
	}

	for scanner.Scan() {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var msg rpcMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			fmt.Fprintf(errlog, "mcp: parse error: %v\n", err)
			writeJSON(rpcError(nil, codeParseError, "invalid JSON"))
			continue
		}
		s.dispatch(ctx, &msg, writeJSON, errlog)
	}
	return scanner.Err()
}

// dispatch routes one inbound message. Requests (id present) get a
// response; notifications (id absent) do not.
func (s *Server) dispatch(ctx context.Context, msg *rpcMessage, writeJSON func(any), errlog io.Writer) {
	switch msg.Method {
	case "initialize":
		writeJSON(s.handleInitialize(msg))
	case "notifications/initialized", "notifications/cancelled":
		// Notifications — no response.
	case "ping":
		if msg.ID != nil {
			writeJSON(rpcResult(msg.ID, map[string]any{}))
		}
	case "tools/list":
		writeJSON(s.handleToolsList(msg))
	case "tools/call":
		writeJSON(s.handleToolsCall(ctx, msg))
	case "prompts/list":
		// No prompts in PR 1; respond with empty list so clients don't error.
		writeJSON(rpcResult(msg.ID, map[string]any{"prompts": []any{}}))
	case "resources/list":
		writeJSON(rpcResult(msg.ID, map[string]any{"resources": []any{}}))
	default:
		if msg.ID != nil {
			writeJSON(rpcError(msg.ID, codeMethodNotFound, "method not supported: "+msg.Method))
		} else {
			fmt.Fprintf(errlog, "mcp: ignoring unknown notification %q\n", msg.Method)
		}
	}
}

// --- handlers ---

func (s *Server) handleInitialize(msg *rpcMessage) any {
	// Per the spec, the server returns its supported protocol version,
	// capabilities, and serverInfo. We support tools (with listChanged=false
	// because our tool set is fixed at startup).
	return rpcResult(msg.ID, map[string]any{
		"protocolVersion": ProtocolVersion,
		"capabilities": map[string]any{
			"tools": map[string]any{"listChanged": false},
		},
		"serverInfo":   s.Info,
		"instructions": s.Instructions,
	})
}

func (s *Server) handleToolsList(msg *rpcMessage) any {
	s.mu.RLock()
	specs := make([]ToolSpec, 0, len(s.tools))
	for _, t := range s.tools {
		specs = append(specs, t.spec)
	}
	s.mu.RUnlock()
	// Sort isn't strictly required, but stable output is friendlier for
	// caching and snapshot tests.
	return rpcResult(msg.ID, map[string]any{"tools": sortedTools(specs)})
}

type toolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

func (s *Server) handleToolsCall(ctx context.Context, msg *rpcMessage) any {
	var params toolCallParams
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		return rpcError(msg.ID, codeInvalidParams, "invalid tools/call params: "+err.Error())
	}
	s.mu.RLock()
	tool, ok := s.tools[params.Name]
	s.mu.RUnlock()
	if !ok {
		return rpcError(msg.ID, codeMethodNotFound, "unknown tool: "+params.Name)
	}
	result, err := tool.handler(ctx, params.Arguments)
	if err != nil {
		// MCP convention: surface tool execution failures as a successful
		// JSON-RPC response with isError: true. The LLM client renders the
		// text content and can choose to retry or abandon — much more
		// useful than a JSON-RPC error, which most clients treat as fatal.
		return rpcResult(msg.ID, map[string]any{
			"isError": true,
			"content": []map[string]any{
				{"type": "text", "text": err.Error()},
			},
		})
	}
	payload, mErr := json.MarshalIndent(result, "", "  ")
	if mErr != nil {
		return rpcError(msg.ID, codeInternalError, "tool result not JSON-encodable: "+mErr.Error())
	}
	return rpcResult(msg.ID, map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": string(payload)},
		},
	})
}

// --- JSON-RPC primitives ---

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcErrorObj    `json:"error,omitempty"`
}

type rpcErrorObj struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// JSON-RPC 2.0 reserved error codes.
const (
	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeInternalError  = -32603
)

func rpcResult(id json.RawMessage, result any) rpcResponse {
	return rpcResponse{JSONRPC: "2.0", ID: id, Result: result}
}

func rpcError(id json.RawMessage, code int, msg string) rpcResponse {
	if id == nil {
		id = json.RawMessage("null")
	}
	return rpcResponse{JSONRPC: "2.0", ID: id, Error: &rpcErrorObj{Code: code, Message: msg}}
}

// sortedTools returns specs sorted by Name for stable output.
func sortedTools(in []ToolSpec) []ToolSpec {
	out := append([]ToolSpec(nil), in...)
	// Simple insertion sort — tool counts are tiny (<20 in practice).
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1].Name > out[j].Name; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}
