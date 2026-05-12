package core

import (
	"errors"
	"fmt"
)

// MoveRequest names a move to apply and supplies its arguments + the player
// claiming to make it. The reducer cross-checks PlayerID against ctx so a
// client can't move out of turn.
type MoveRequest struct {
	PlayerID string `json:"playerID"`
	Move     string `json:"move"`
	Args     []any  `json:"args"`
}

// ErrWrongPlayer means a player tried to move when it wasn't their turn.
var ErrWrongPlayer = errors.New("not your turn")

// ErrUnknownMove means the move name isn't in the Game's Moves table.
var ErrUnknownMove = errors.New("unknown move")

// ErrGameOver means the match has already ended; no further moves are
// accepted.
var ErrGameOver = errors.New("game is over")

// Apply is the engine's only mutating operation. It takes the current state
// and a request, validates the request, runs the matching MoveFn, applies
// EndIf, advances the turn, and returns the next state.
//
// On any validation error or MoveFn error the returned State is the original
// (no partial application).
func Apply(game *Game, state State, req MoveRequest) (State, error) {
	if state.Ctx.GameOver {
		return state, ErrGameOver
	}
	if req.PlayerID != state.Ctx.CurrentPlayer {
		return state, fmt.Errorf("%w: current=%s got=%s",
			ErrWrongPlayer, state.Ctx.CurrentPlayer, req.PlayerID)
	}
	fn, ok := game.Moves[req.Move]
	if !ok {
		return state, fmt.Errorf("%w: %q", ErrUnknownMove, req.Move)
	}

	nextG, err := fn(state.G, state.Ctx, req.Args...)
	if err != nil {
		return state, err
	}

	next := state
	next.G = nextG

	if game.EndIf != nil {
		over, winner, draw := game.EndIf(next.G, next.Ctx)
		if over {
			next.Ctx.GameOver = true
			next.Ctx.Winner = winner
			next.Ctx.IsDraw = draw
			return next, nil
		}
	}

	next.Ctx = advanceTurn(next.Ctx)
	return next, nil
}

// advanceTurn moves the cursor to the next seat in PlayOrder and bumps the
// turn counter. With no phases/stages this is just round-robin.
func advanceTurn(ctx Ctx) Ctx {
	idx := 0
	for i, p := range ctx.PlayOrder {
		if p == ctx.CurrentPlayer {
			idx = i
			break
		}
	}
	ctx.CurrentPlayer = ctx.PlayOrder[(idx+1)%len(ctx.PlayOrder)]
	ctx.Turn++
	return ctx
}
