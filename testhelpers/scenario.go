// Package testhelpers contains utilities for writing unit tests against
// boardgame-go games. None of this is for production: it's all about
// constructing specific scenarios, faking randomness, and asserting
// against engine state.
package testhelpers

import "github.com/tjcran/boardgame-go/core"

// Scenario starts a match in a specific user-defined state. The Game's
// Setup is bypassed — the supplied G replaces it — but every other lifecycle
// hook (plugin Setup, phase OnBegin, turn OnBegin) still runs.
//
// Use this in tests when you want to verify a move from a contrived
// mid-game state without playing through the preceding turns.
//
//	g := myGame()
//	state := testhelpers.Scenario(g, &MyG{ Score: 5 }, testhelpers.CtxOverrides{
//	    CurrentPlayer: "1",
//	    Phase:         "play",
//	})
//	next, _ := core.Apply(g, state, core.MoveRequest{...})
func Scenario(game *core.Game, g core.G, opts CtxOverrides) core.State {
	state := core.NewMatch(game, opts.NumPlayers, nil)
	state.G = g
	if opts.CurrentPlayer != "" {
		state.Ctx.CurrentPlayer = opts.CurrentPlayer
		for i, p := range state.Ctx.PlayOrder {
			if p == opts.CurrentPlayer {
				state.Ctx.PlayOrderPos = i
				break
			}
		}
	}
	if opts.Phase != "" {
		state.Ctx.Phase = opts.Phase
	}
	if opts.Turn > 0 {
		state.Ctx.Turn = opts.Turn
	}
	if opts.ActivePlayers != nil {
		state.Ctx.ActivePlayers = opts.ActivePlayers
	}
	return state
}

// CtxOverrides supplies optional starting values to Scenario. Zero-valued
// fields are left at their NewMatch defaults.
type CtxOverrides struct {
	NumPlayers    int
	CurrentPlayer string
	Phase         string
	Turn          int
	ActivePlayers map[string]string
}
