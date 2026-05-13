package llm

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tjcran/boardgame-go/bots"
)

// ActionDescriber returns a human-readable description of an Action.
// Used to populate the Description field on the tool schema so the LLM
// understands what each option does. nil → the engine generates a
// generic description from Move + Args.
type ActionDescriber func(action bots.Action) string

// ToolsFromActions turns a slice of legal Actions into a tool schema
// the LLM can pick among. Each Action becomes one no-parameter tool;
// the LLM picks one tool by name and we parse the name back into the
// Action.
//
// This approach avoids prompt injection on action choice: the model
// can't return arbitrary JSON for args because there ARE no args to
// supply — the engine has already enumerated every option.
//
// Tool names are URL-safe and unambiguous: "action_<index>_<move>"
// where index is the action's position in the input slice. We carry
// the index so multiple actions with the same Move name (different
// args) stay distinguishable.
func ToolsFromActions(actions []bots.Action, describe ActionDescriber) []Tool {
	if describe == nil {
		describe = defaultActionDescriber
	}
	tools := make([]Tool, len(actions))
	emptyParams := json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)
	for i, a := range actions {
		tools[i] = Tool{
			Name:        toolName(i, a.Move),
			Description: describe(a),
			Parameters:  emptyParams,
		}
	}
	return tools
}

// ParseToolCallToAction extracts which Action the LLM picked from a
// ChatResponse. Returns the corresponding bots.Action from the
// original slice or an error if the response had no tool calls / the
// chosen tool name doesn't match any action.
func ParseToolCallToAction(resp ChatResponse, actions []bots.Action) (bots.Action, error) {
	if len(resp.ToolCalls) == 0 {
		return bots.Action{}, fmt.Errorf("llm: model returned no tool call (finish_reason=%s, content=%q)",
			resp.FinishReason, resp.Content)
	}
	// Use the first tool call. Some providers can return parallel
	// calls; we pick one — game authors who want richer multi-call
	// handling can read resp.ToolCalls directly.
	picked := resp.ToolCalls[0]
	idx, move, ok := parseToolName(picked.Name)
	if !ok {
		return bots.Action{}, fmt.Errorf("llm: tool name %q has unexpected shape (want action_<idx>_<move>)", picked.Name)
	}
	if idx < 0 || idx >= len(actions) {
		return bots.Action{}, fmt.Errorf("llm: tool index %d out of range (have %d actions)", idx, len(actions))
	}
	chosen := actions[idx]
	if chosen.Move != move {
		// Defensive — should not happen given we built the name
		// from the action — but worth catching if a provider
		// scrambles the name.
		return bots.Action{}, fmt.Errorf("llm: tool name %q doesn't match action %q at index %d",
			picked.Name, chosen.Move, idx)
	}
	return chosen, nil
}

// toolName generates a deterministic, URL-safe name for one Action.
// Pattern: "action_<idx>_<move-with-underscores>".
func toolName(idx int, move string) string {
	safe := strings.ReplaceAll(move, " ", "_")
	safe = strings.ReplaceAll(safe, "-", "_")
	return fmt.Sprintf("action_%d_%s", idx, safe)
}

// parseToolName is the inverse of toolName. Returns (index, move, ok).
func parseToolName(name string) (int, string, bool) {
	const prefix = "action_"
	if !strings.HasPrefix(name, prefix) {
		return 0, "", false
	}
	rest := name[len(prefix):]
	sep := strings.IndexByte(rest, '_')
	if sep < 0 {
		return 0, "", false
	}
	idxStr := rest[:sep]
	move := rest[sep+1:]
	idx := 0
	for _, c := range idxStr {
		if c < '0' || c > '9' {
			return 0, "", false
		}
		idx = idx*10 + int(c-'0')
	}
	return idx, move, true
}

// defaultActionDescriber generates a generic description for an
// Action. Better-than-nothing; game authors should supply their own
// describer that says what each move actually means.
func defaultActionDescriber(a bots.Action) string {
	if len(a.Args) == 0 {
		return "Play move: " + a.Move
	}
	parts := make([]string, len(a.Args))
	for i, v := range a.Args {
		parts[i] = fmt.Sprint(v)
	}
	return fmt.Sprintf("Play move: %s with args [%s]", a.Move, strings.Join(parts, ", "))
}
