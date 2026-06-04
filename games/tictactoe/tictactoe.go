// Package tictactoe is the reference game built on the boardgame-go engine.
// Two players alternate placing X / O on a 3x3 board; first row/col/diagonal
// of three wins, full board with no winner is a draw.
package tictactoe

import (
	"encoding/json"
	"fmt"

	"github.com/tjcran/boardgame-go/core"
)

// State is the game-specific G payload. Cells holds each square's mark as
// "X", "O", or "" (empty). Index 0 is top-left, 8 is bottom-right.
type State struct {
	Cells [9]string `json:"cells"`
}

// New returns the registered Game definition. Pass to a match.Manager.
func New() *core.Game {
	return &core.Game{
		Name:       "tic-tac-toe",
		MinPlayers: 2,
		MaxPlayers: 2,
		Setup:      func(_ core.Ctx, _ any) core.G { return &State{} },
		DecodeG: func(raw json.RawMessage) (core.G, error) {
			var s State
			if err := json.Unmarshal(raw, &s); err != nil {
				return nil, err
			}
			return &s, nil
		},
		Moves: map[string]any{
			"clickCell": core.MoveFn(clickCell),
		},
		// Tic-tac-toe: one move per turn. Matches BGIO's tutorial which
		// explicitly sets these — without them, turns would be multi-move
		// and require an explicit events.EndTurn() call.
		Turn: &core.TurnConfig{
			MinMoves: 1,
			MaxMoves: 1,
		},
		EndIf:     endIf,
		Enumerate: enumerate,
	}
}

// enumerate lists every empty cell as a legal clickCell move for the
// current player. Returning nil for non-current players matches the
// expectation of the bots and MCP layers — only the player on turn has
// legal actions to choose from.
func enumerate(g core.G, ctx core.Ctx, playerID string) []core.EnumerateAction {
	if playerID != ctx.CurrentPlayer {
		return nil
	}
	s := g.(*State)
	out := make([]core.EnumerateAction, 0, 9)
	for i, c := range s.Cells {
		if c == "" {
			out = append(out, core.EnumerateAction{Move: "clickCell", Args: []any{i}})
		}
	}
	return out
}

// clickCell expects one int argument: the cell index (0..8). The current
// player's mark is X if seat 0, O if seat 1.
func clickCell(mc *core.MoveContext, args ...any) (core.G, error) {
	if len(args) < 1 {
		return mc.G, fmt.Errorf("%w: missing cell index", core.ErrInvalidMove)
	}
	idx, err := toInt(args[0])
	if err != nil {
		return mc.G, fmt.Errorf("%w: %v", core.ErrInvalidMove, err)
	}
	if idx < 0 || idx > 8 {
		return mc.G, fmt.Errorf("%w: cell %d out of range", core.ErrInvalidMove, idx)
	}
	s := mc.G.(*State)
	if s.Cells[idx] != "" {
		return mc.G, fmt.Errorf("%w: cell %d already taken", core.ErrInvalidMove, idx)
	}
	mark := "X"
	if mc.Ctx.CurrentPlayer == "1" {
		mark = "O"
	}
	next := *s
	next.Cells[idx] = mark
	return &next, nil
}

func endIf(mc *core.MoveContext) any {
	s := mc.G.(*State)
	if w := winner(s.Cells); w != "" {
		return map[string]any{"winner": mc.Ctx.CurrentPlayer}
	}
	full := true
	for _, c := range s.Cells {
		if c == "" {
			full = false
			break
		}
	}
	if full {
		return map[string]any{"draw": true}
	}
	return nil
}

var winningLines = [...][3]int{
	{0, 1, 2}, {3, 4, 5}, {6, 7, 8},
	{0, 3, 6}, {1, 4, 7}, {2, 5, 8},
	{0, 4, 8}, {2, 4, 6},
}

func winner(cells [9]string) string {
	for _, line := range winningLines {
		a, b, c := cells[line[0]], cells[line[1]], cells[line[2]]
		if a != "" && a == b && b == c {
			return a
		}
	}
	return ""
}

// toInt accepts the loose JSON-decoded forms (float64, int, json.Number) and
// returns a plain int. Args come over the wire as floats because
// encoding/json decodes numbers into float64 by default.
func toInt(v any) (int, error) {
	switch n := v.(type) {
	case int:
		return n, nil
	case int32:
		return int(n), nil
	case int64:
		return int(n), nil
	case float64:
		return int(n), nil
	case float32:
		return int(n), nil
	default:
		return 0, fmt.Errorf("not an int: %T", v)
	}
}
