package mcp

import (
	"encoding/json"
	"io"
	"net/http"
)

// HTTPHandler returns an http.Handler that serves the MCP JSON-RPC
// protocol over a single endpoint. Mount it at whatever path your service
// exposes:
//
//	mux := http.NewServeMux()
//	mux.Handle("/mcp", srv.HTTPHandler())
//
// One JSON-RPC frame per request, one response per frame. Notifications
// (no id) return 202 Accepted with an empty body, per the MCP
// Streamable-HTTP convention. Bodies are capped at 4 MiB — the same
// ceiling the stdio transport allows — to bound memory per request.
//
// Authentication is the middleware's job (see AuthMiddleware). This
// handler trusts whatever userID has been attached to the request
// context; chain it behind an authenticator when serving to the public
// internet.
func (s *Server) HTTPHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		defer r.Body.Close()

		body, err := io.ReadAll(io.LimitReader(r.Body, 4*1024*1024))
		if err != nil {
			http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
			return
		}
		if len(body) == 0 {
			writeJSONResponse(w, rpcError(nil, codeInvalidRequest, "empty body"))
			return
		}

		var msg rpcMessage
		if err := json.Unmarshal(body, &msg); err != nil {
			writeJSONResponse(w, rpcError(nil, codeParseError, "invalid JSON"))
			return
		}

		// dispatch is shared with the stdio transport. Notifications skip
		// the writeJSON callback entirely, so captured stays nil and we
		// return 202 Accepted below.
		var captured any
		writeOnce := func(v any) {
			if captured == nil {
				captured = v
			}
		}
		s.dispatch(r.Context(), &msg, writeOnce, io.Discard)

		if captured == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		writeJSONResponse(w, captured)
	})
}

func writeJSONResponse(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
