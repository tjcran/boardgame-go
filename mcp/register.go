package mcp

import (
	"context"
	"encoding/json"
)

// DefaultInstructions is the umbrella "how to play any registered game"
// guidance baked into the server. Clients show this to the LLM as
// system-level context so it doesn't need a per-game skill to play
// competently.
const DefaultInstructions = `You can play board games against a human via this server.

To play any game, follow this loop:

1. Call list_games to see what's available.
2. Call create_match to start a fresh match (the human will tell you which game).
3. Call join_match to claim a seat. Remember the playerID and credentials it
   returns — every subsequent move needs them.
4. Call get_state to see the board and whose turn it is.
5. When it is your turn (state.currentPlayer matches your playerID):
   - Call list_legal_moves to get the exact set of moves you may play.
   - Pick one and call make_move with the returned move name and args.
   - Narrate your move to the human in plain language ("I'll play center.").
6. When it is the human's turn, wait. Poll get_state to see when they have
   moved.
7. When state.gameover is non-nil the match is over. Read it and announce
   the result.

Rules:
- Never invent move arguments. Always use what list_legal_moves returned.
- Never call make_move when it isn't your turn — the server will reject it.
- If the human asks you to play a specific opening or strategy, follow their
  guidance.
- If a tool returns an error, read it carefully — the message tells you what
  went wrong (occupied cell, out of turn, bad credentials, etc.).`

// RegisterTools installs the gameplay tools on a Server.
//
// JSON Schemas describe each tool's arguments so MCP clients can render
// argument pickers and pre-validate calls. They are deliberately tight:
// every required field is listed in `required`, and additionalProperties
// is false where appropriate, so the LLM gets a clear schema to follow.
func RegisterTools(s *Server, t *Tools) {
	s.RegisterTool(ToolSpec{
		Name:        "list_games",
		Description: "List all registered games on this server, with their player-count bounds.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
	}, wrap(func(ctx context.Context, _ json.RawMessage) (any, error) {
		return t.ListGames(ctx)
	}))

	s.RegisterTool(ToolSpec{
		Name:        "create_match",
		Description: "Start a fresh match of the named game. Returns the matchID required to join and play.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"game":       {"type": "string", "description": "Name of a game from list_games."},
				"numPlayers": {"type": "integer", "description": "Number of players. Falls back to the game's default if omitted.", "minimum": 1},
				"name":       {"type": "string", "description": "Optional human-readable label for the match."},
				"setupData":  {"description": "Game-specific setup options. Most games accept null."}
			},
			"required": ["game"],
			"additionalProperties": false
		}`),
	}, wrap(func(ctx context.Context, raw json.RawMessage) (any, error) {
		var args CreateMatchArgs
		if err := unmarshal(raw, &args); err != nil {
			return nil, err
		}
		return t.CreateMatch(ctx, args)
	}))

	s.RegisterTool(ToolSpec{
		Name:        "join_match",
		Description: "Claim a seat at a match. Returns a playerID and credentials you must include on every subsequent make_move call.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"matchID":  {"type": "string"},
				"playerID": {"type": "string", "description": "Optional. Claim a specific seat ('0', '1', ...). Defaults to the first free seat."},
				"name":     {"type": "string", "description": "Optional display name for this seat."}
			},
			"required": ["matchID"],
			"additionalProperties": false
		}`),
	}, wrap(func(ctx context.Context, raw json.RawMessage) (any, error) {
		var args JoinMatchArgs
		if err := unmarshal(raw, &args); err != nil {
			return nil, err
		}
		return t.JoinMatch(ctx, args)
	}))

	s.RegisterTool(ToolSpec{
		Name:        "get_state",
		Description: "Fetch the current match state, redacted to the given player's view. Includes whose turn it is and gameover info.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"matchID":  {"type": "string"},
				"playerID": {"type": "string", "description": "Whose perspective to render. Empty/omitted gets the spectator view."}
			},
			"required": ["matchID"],
			"additionalProperties": false
		}`),
	}, wrap(func(ctx context.Context, raw json.RawMessage) (any, error) {
		var args GetStateArgs
		if err := unmarshal(raw, &args); err != nil {
			return nil, err
		}
		return t.GetState(ctx, args)
	}))

	s.RegisterTool(ToolSpec{
		Name:        "list_legal_moves",
		Description: "List every legal (move, args) pair the given player may submit right now. Always call this before make_move.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"matchID":  {"type": "string"},
				"playerID": {"type": "string"}
			},
			"required": ["matchID", "playerID"],
			"additionalProperties": false
		}`),
	}, wrap(func(ctx context.Context, raw json.RawMessage) (any, error) {
		var args ListLegalMovesArgs
		if err := unmarshal(raw, &args); err != nil {
			return nil, err
		}
		return t.ListLegalMoves(ctx, args)
	}))

	s.RegisterTool(ToolSpec{
		Name:        "make_move",
		Description: "Submit a move. Use exactly the move name and args returned by list_legal_moves. Returns the new state.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"matchID":     {"type": "string"},
				"playerID":    {"type": "string"},
				"credentials": {"type": "string", "description": "The credentials returned by join_match for this seat."},
				"move":        {"type": "string"},
				"args":        {"type": "array", "description": "Move arguments. Use exactly what list_legal_moves returned for this move."},
				"resumeTag":   {"type": "string", "description": "Set to a pending block's tag (its TargetRequest kind) to resume a paused move with this move as the selection."}
			},
			"required": ["matchID", "playerID", "credentials", "move"],
			"additionalProperties": false
		}`),
	}, wrap(func(ctx context.Context, raw json.RawMessage) (any, error) {
		var args MakeMoveArgs
		if err := unmarshal(raw, &args); err != nil {
			return nil, err
		}
		return t.MakeMove(ctx, args)
	}))

	s.RegisterTool(ToolSpec{
		Name:        "register_game",
		Description: "Register a brand-new game designed in this session. The source is a Starlark module following the spec defined in the design-a-game prompt; llm_guide is optional markdown surfaced as a game://owner/name/guide MCP resource. Returns the canonical name from META.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"source":    {"type": "string", "description": "Starlark module source (UTF-8)."},
				"llm_guide": {"type": "string", "description": "Optional markdown explaining the rules and strategy hints."}
			},
			"required": ["source"],
			"additionalProperties": false
		}`),
	}, wrap(func(ctx context.Context, raw json.RawMessage) (any, error) {
		var args RegisterGameArgs
		if err := unmarshal(raw, &args); err != nil {
			return nil, err
		}
		return t.RegisterGame(ctx, args)
	}))

	s.RegisterTool(ToolSpec{
		Name:        "delete_game",
		Description: "Delete a game you previously designed. You can only delete games you own; built-ins are protected.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {"name": {"type": "string"}},
			"required": ["name"],
			"additionalProperties": false
		}`),
	}, wrap(func(ctx context.Context, raw json.RawMessage) (any, error) {
		var args DeleteGameArgs
		if err := unmarshal(raw, &args); err != nil {
			return nil, err
		}
		return t.DeleteGame(ctx, args)
	}))

	s.RegisterTool(ToolSpec{
		Name:        "export_game",
		Description: "Export a game you previously designed as a skill-shaped package: a SKILL.md skeleton (frontmatter + auto-rendered moves table + the designer's llm_guide), the Starlark spec source, and a structured manifest. Use this to share a designed game, seed a per-game Claude skill, or back up a spec outside the server. Built-ins can't be exported. Cross-owner exports are refused.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {"name": {"type": "string", "description": "Public name of a game you registered earlier."}},
			"required": ["name"],
			"additionalProperties": false
		}`),
	}, wrap(func(ctx context.Context, raw json.RawMessage) (any, error) {
		var args ExportGameArgs
		if err := unmarshal(raw, &args); err != nil {
			return nil, err
		}
		return t.ExportGame(ctx, args)
	}))

	s.RegisterTool(ToolSpec{
		Name:        "playtest_draft",
		Description: "Dry-run a draft game spec. Returns validation errors, the initial state, and a per-step trace (state before/after, end_if result, legal moves) for the optional scenario. Side-effect-free; no DB write.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"source":   {"type": "string", "description": "Starlark module source (UTF-8) of a draft spec."},
				"scenario": {
					"type": "array",
					"items": {
						"type": "object",
						"properties": {
							"player_id": {"type": "string"},
							"move":      {"type": "string"},
							"args":      {"type": "array"}
						},
						"required": ["player_id", "move"],
						"additionalProperties": false
					}
				}
			},
			"required": ["source"],
			"additionalProperties": false
		}`),
	}, wrap(func(ctx context.Context, raw json.RawMessage) (any, error) {
		var args PlaytestDraftArgs
		if err := unmarshal(raw, &args); err != nil {
			return nil, err
		}
		return t.PlaytestDraft(ctx, args)
	}))

	s.RegisterTool(ToolSpec{
		Name:        "module_op",
		Description: "Design-time: invoke an engine-module operation on a draft match's live module state. Lets you prototype mechanics interactively; runs the exact same op the Starlark binding uses.",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"matchId": {"type": "string", "description": "Match whose live module state to operate on."},
				"module":  {"type": "string", "description": "Engine module name, e.g. \"ccg\"."},
				"op":      {"type": "string", "description": "Op name within the module, e.g. \"new_zone\"."},
				"args":    {"type": "object", "description": "Op arguments."}
			},
			"required": ["matchId", "module", "op"],
			"additionalProperties": false
		}`),
	}, wrap(func(ctx context.Context, raw json.RawMessage) (any, error) {
		var args ModuleOpArgs
		if err := unmarshal(raw, &args); err != nil {
			return nil, err
		}
		return t.ModuleOp(ctx, args)
	}))
}

// wrap is a tiny adapter so handler bodies above can return (any, error)
// without restating the ToolHandler signature.
func wrap(fn func(context.Context, json.RawMessage) (any, error)) ToolHandler {
	return fn
}

// unmarshal decodes raw into v, tolerating an empty arguments object
// (common when an LLM omits required fields — we'd rather return a
// helpful tool error than a JSON parse error).
func unmarshal(raw json.RawMessage, v any) error {
	if len(raw) == 0 {
		raw = json.RawMessage("{}")
	}
	return json.Unmarshal(raw, v)
}
