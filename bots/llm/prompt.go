package llm

import (
	"fmt"
	"strings"

	"github.com/tjcran/boardgame-go/bots"
	"github.com/tjcran/boardgame-go/core"
)

// DefaultPrompt is a minimal starting prompt builder. It mentions the
// turn number, the current player, and lists the legal actions. Good
// enough for trivial games (tic-tac-toe-scale); insufficient for
// anything with non-obvious state.
//
// Real games should write their own PromptFn that includes a board
// rendering, recent moves, scores, hidden information visible to the
// player, etc. — anything the LLM needs to make a good decision.
//
//	bot.PromptFn = llm.DefaultPrompt  // OK for prototyping
//	bot.PromptFn = myGameSpecificPrompt  // recommended for real use
func DefaultPrompt(state core.State, playerID string, actions []bots.Action) (string, string) {
	system := "You are playing a turn-based game. On your turn, pick exactly one legal action by calling the corresponding tool. Choose strategically."

	var user strings.Builder
	fmt.Fprintf(&user, "It is turn %d. You are player %s.\n",
		state.Ctx.Turn, playerID)
	if state.Ctx.Phase != "" {
		fmt.Fprintf(&user, "Current phase: %s.\n", state.Ctx.Phase)
	}
	user.WriteString("\nGame state (raw):\n")
	fmt.Fprintf(&user, "%+v\n\n", state.G)
	user.WriteString("Your legal actions:\n")
	for i, a := range actions {
		fmt.Fprintf(&user, "  %d. %s", i, a.Move)
		if len(a.Args) > 0 {
			fmt.Fprintf(&user, " %v", a.Args)
		}
		user.WriteByte('\n')
	}
	user.WriteString("\nCall the tool corresponding to your chosen action.")

	return system, user.String()
}
