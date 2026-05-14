package mcp

import (
	"context"
	"encoding/json"
	"fmt"
)

// PromptArgument describes one named argument a prompt accepts. Clients
// render argument pickers from this list.
type PromptArgument struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

// PromptSpec is the metadata MCP clients see in prompts/list.
type PromptSpec struct {
	Name        string           `json:"name"`
	Description string           `json:"description"`
	Arguments   []PromptArgument `json:"arguments,omitempty"`
}

// PromptMessage is one entry in a prompt's expansion. Role is "user" or
// "assistant"; content is a text/image/resource block. We use text only.
type PromptMessage struct {
	Role    string                 `json:"role"`
	Content map[string]interface{} `json:"content"`
}

// PromptHandler returns the messages to materialize when a client invokes
// prompts/get for this prompt. args is the JSON object the client supplied.
type PromptHandler func(ctx context.Context, args json.RawMessage) ([]PromptMessage, error)

type registeredPrompt struct {
	spec    PromptSpec
	handler PromptHandler
}

// RegisterPrompt adds a prompt to the server. Hosted-mode users (Claude.ai
// connectors) see registered prompts in the prompt picker; this is how we
// replace per-game Claude Code skill files in the hosted deployment.
func (s *Server) RegisterPrompt(spec PromptSpec, handler PromptHandler) {
	s.mu.Lock()
	if s.prompts == nil {
		s.prompts = map[string]registeredPrompt{}
	}
	s.prompts[spec.Name] = registeredPrompt{spec: spec, handler: handler}
	s.mu.Unlock()
}

func (s *Server) handlePromptsList(msg *rpcMessage) any {
	s.mu.RLock()
	specs := make([]PromptSpec, 0, len(s.prompts))
	for _, p := range s.prompts {
		specs = append(specs, p.spec)
	}
	s.mu.RUnlock()
	return rpcResult(msg.ID, map[string]any{"prompts": sortedPrompts(specs)})
}

type promptGetParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

func (s *Server) handlePromptsGet(ctx context.Context, msg *rpcMessage) any {
	var params promptGetParams
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		return rpcError(msg.ID, codeInvalidParams, "invalid prompts/get params: "+err.Error())
	}
	s.mu.RLock()
	prompt, ok := s.prompts[params.Name]
	s.mu.RUnlock()
	if !ok {
		return rpcError(msg.ID, codeMethodNotFound, "unknown prompt: "+params.Name)
	}
	messages, err := prompt.handler(ctx, params.Arguments)
	if err != nil {
		return rpcError(msg.ID, codeInternalError, fmt.Sprintf("prompt %q failed: %v", params.Name, err))
	}
	return rpcResult(msg.ID, map[string]any{
		"description": prompt.spec.Description,
		"messages":    messages,
	})
}

// textMessage is the workhorse constructor for prompt expansions — one
// user-role message with a single text block.
func textMessage(text string) PromptMessage {
	return PromptMessage{
		Role:    "user",
		Content: map[string]interface{}{"type": "text", "text": text},
	}
}

func sortedPrompts(in []PromptSpec) []PromptSpec {
	out := append([]PromptSpec(nil), in...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1].Name > out[j].Name; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}

// RegisterDefaultPrompts installs the play-<game> prompts that the hosted
// MCP server exposes. Stdio-mode users typically don't see these — they
// install the Claude Code skill files under mcp/skills/ instead — but
// registering them in both modes keeps the wire surface consistent.
func RegisterDefaultPrompts(s *Server) {
	s.RegisterPrompt(PromptSpec{
		Name:        "play-tictactoe",
		Description: "Play a game of tic-tac-toe against the human via this MCP server. Primes Claude with the protocol and a solid strategy.",
	}, func(_ context.Context, _ json.RawMessage) ([]PromptMessage, error) {
		return []PromptMessage{textMessage(playTicTacToeText)}, nil
	})
}

const playTicTacToeText = `You are about to play tic-tac-toe against the human via the boardgame-mcp server.

Setup:
1. Call create_match with game="tic-tac-toe" and numPlayers=2.
2. Call join_match to claim seat 0 (X). Remember the playerID and credentials.
3. Ask the human which seat they want — or if they haven't said, assume seat 1 (O).
4. Optionally call join_match a second time on their behalf to seat them; otherwise
   leave seat 1 unclaimed and pass their moves through as Claude-on-behalf-of-O.

Board indexing (clickCell args):
   0 | 1 | 2
  -----------
   3 | 4 | 5
  -----------
   6 | 7 | 8

Strategy (priority order, pick the first that applies):
1. Win if you can — play a cell that completes three-in-a-row of your mark.
2. Block if you must — if the human has two in a row with the third cell open, take it.
3. Fork if you can — create two simultaneous threats.
4. Block forks — prevent the human's fork.
5. Center (4) — if open, take it.
6. Opposite corner of the human's last corner.
7. Empty corner.
8. Empty edge — last resort.

Loop:
- call get_state(playerID=<your seat>)
- if state.gameover != null → announce the result, congratulate or commiserate, offer a rematch
- if state.currentPlayer == your seat:
    - call list_legal_moves(playerID=<your seat>)
    - pick by the priority list above; narrate your reasoning in one short line
    - call make_move(matchID, playerID, credentials, move="clickCell", args=[cell])
- else:
    - ask the human for their move
    - when they tell you, call make_move on their behalf (matchID, playerID=their seat, credentials=their seat's creds, move="clickCell", args=[cell])

Rules:
- Never invent move arguments. Use exactly what list_legal_moves returned.
- Never call make_move when it isn't that seat's turn.
- If the human asks for an opening (e.g. "play 0"), follow their guidance.`
