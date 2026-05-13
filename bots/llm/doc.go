// Package llm wires Large Language Model providers (OpenAI / Anthropic /
// OpenRouter and friends) into the bots.Bot interface. Provider clients
// are thin HTTP — no vendor SDKs — so the dependency surface stays
// minimal and what we send is auditable from the test suite.
//
// The bot is hook-based: the engine has no opinion on prompting. You
// supply a PromptFn that turns (state, playerID, legal-actions) into
// system + user prompts; the engine ships the call, parses the LLM's
// tool choice back into a bots.Action, and validates it against the
// enumerated legal set before returning.
//
// LLM bots are NOT replay-safe. Provider model versions change;
// internal batching affects outputs even at temperature=0. Use the
// non-LLM bots (RandomBot, MCTSBot) for byte-deterministic replay.
//
// Minimal sketch:
//
//	prov := llm.NewAnthropicProvider(llm.AnthropicOpts{APIKey: os.Getenv("ANTHROPIC_API_KEY")})
//	bot := &llm.LLMBot{
//	    Provider: prov,
//	    Model:    "claude-opus-4-7",
//	    Game:     myGame,
//	    PromptFn: func(state core.State, pid string, actions []bots.Action) (string, string) {
//	        return "You are a careful tic-tac-toe player.",
//	            fmt.Sprintf("Board:\n%s\nYour turn (%s). Pick a move.", render(state), pid)
//	    },
//	}
//	action, err := bot.Play(ctx, state, playerID)
package llm
