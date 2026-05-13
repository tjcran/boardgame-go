package llm

import (
	"context"
	"errors"
	"fmt"

	"github.com/tjcran/boardgame-go/bots"
	"github.com/tjcran/boardgame-go/core"
)

// ToolMode controls how LLMBot wires tool calling into the request.
type ToolMode int

const (
	// ToolModeRequired forces the model to call one of the supplied
	// tools (the enumerated legal actions). Default — most reliable
	// way to get a structured action out of an LLM.
	ToolModeRequired ToolMode = iota
	// ToolModeAuto lets the model decide whether to call a tool or
	// respond with text. Useful when you want the model to "think
	// out loud" before committing.
	ToolModeAuto
	// ToolModeFree disables tool calling. The model returns prose
	// and LLMBot.ParseFreeText (which the user must supply) extracts
	// an Action. Fragile; prefer ToolModeRequired unless you have a
	// reason.
	ToolModeFree
)

// PromptFn builds the conversation for one Bot.Play. Receives the
// current state, the playerID making the move, and the legal Actions
// (already enumerated via game.Enumerate). Returns (system, user)
// prompts; LLMBot sends them as Messages.
type PromptFn func(state core.State, playerID string, actions []bots.Action) (system, user string)

// LLMBot is the engine's LLM-backed Bot. Implements bots.Bot.
//
// Required fields: Provider, Model, Game, PromptFn. All others have
// sensible defaults.
type LLMBot struct {
	Provider Provider
	Model    string
	Game     *core.Game

	// Enumerate, when set, overrides Game.Enumerate.
	Enumerate bots.EnumerateFn

	// PromptFn builds system + user messages from the current state.
	// Required.
	PromptFn PromptFn

	// Describe is called per Action to populate each tool's
	// description field. Optional; defaults to a generic
	// "Play move: X with args [Y]" description.
	Describe ActionDescriber

	// ToolMode controls tool-vs-text behaviour. Default Required.
	ToolMode ToolMode

	// ParseFreeText runs in ToolModeFree to turn the LLM's prose
	// reply into an Action. Required when ToolMode is Free.
	ParseFreeText func(content string, actions []bots.Action) (bots.Action, error)

	// Temperature for the request. Default 0.
	Temperature float64

	// MaxTokens caps the response length. Default 1024.
	MaxTokens int

	// MaxRetries caps how many times we'll retry on provider errors.
	// Default 2 (= 3 attempts total).
	MaxRetries int

	// RetryFn, if set, is called on every provider error. Returning
	// false stops retries; returning true continues. nil = retry up
	// to MaxRetries.
	RetryFn func(attempt int, err error) bool
}

// Play implements bots.Bot.
func (b *LLMBot) Play(ctx context.Context, state core.State, playerID string) (bots.Action, error) {
	if b.Provider == nil {
		return bots.Action{}, errors.New("LLMBot: Provider is required")
	}
	if b.Model == "" {
		return bots.Action{}, errors.New("LLMBot: Model is required")
	}
	if b.Game == nil {
		return bots.Action{}, errors.New("LLMBot: Game is required")
	}
	if b.PromptFn == nil {
		return bots.Action{}, errors.New("LLMBot: PromptFn is required")
	}
	enum := b.Enumerate
	if enum == nil {
		enum = b.Game.Enumerate
	}
	if enum == nil {
		return bots.Action{}, errors.New("LLMBot: no Enumerate function (set bot.Enumerate or game.Enumerate)")
	}
	actions := enum(state.G, state.Ctx, playerID)
	if len(actions) == 0 {
		return bots.Action{}, bots.ErrNoMoves
	}

	system, user := b.PromptFn(state, playerID, actions)
	req := ChatRequest{
		Model:       b.Model,
		System:      system,
		Messages:    []Message{{Role: RoleUser, Content: user}},
		Temperature: b.Temperature,
		MaxTokens:   nonZeroInt(b.MaxTokens, 1024),
	}
	if b.ToolMode != ToolModeFree {
		req.Tools = ToolsFromActions(actions, b.Describe)
		if b.ToolMode == ToolModeRequired {
			req.ToolChoice = ToolChoiceRequired
		} else {
			req.ToolChoice = ToolChoiceAuto
		}
	}

	maxRetries := b.MaxRetries
	if maxRetries == 0 {
		maxRetries = 2
	}
	var resp ChatResponse
	var err error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		resp, err = b.Provider.Chat(ctx, req)
		if err == nil {
			break
		}
		if ctx.Err() != nil {
			return bots.Action{}, ctx.Err()
		}
		if b.RetryFn != nil && !b.RetryFn(attempt, err) {
			break
		}
		if attempt == maxRetries {
			break
		}
	}
	if err != nil {
		return bots.Action{}, fmt.Errorf("llm: provider call failed after %d attempts: %w", maxRetries+1, err)
	}

	if b.ToolMode == ToolModeFree {
		if b.ParseFreeText == nil {
			return bots.Action{}, errors.New("llm: ToolModeFree set but ParseFreeText is nil")
		}
		return b.ParseFreeText(resp.Content, actions)
	}

	action, err := ParseToolCallToAction(resp, actions)
	if err != nil {
		return bots.Action{}, err
	}
	return action, nil
}

func nonZeroInt(v, def int) int {
	if v == 0 {
		return def
	}
	return v
}
