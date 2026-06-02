// Package risklike is a 2-player toy game that validates two
// coexisting modules/tabletop/regions Maps on the same board: a
// territory map (per-cell control) and a continent map (cell groups
// of territories).
//
// Board: 8x8 SquareBoard. 6 named territories live on row 0 (north)
// and row 1 (south), grouped into 2 continents:
//
//	north continent: (0,0) (1,0) (2,0)
//	south continent: (0,1) (1,1) (2,1)
//
// Players alternate placing one unit per turn for 8 turns. End-of-
// game scoring: 1pt per controlled territory (Plurality, NoAward on
// tie) + 3pt per fully-controlled continent (Threshold = 3, counted
// via influence across continent cells).
package risklike

import (
	"fmt"

	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/modules/ccg"
	"github.com/tjcran/boardgame-go/modules/tabletop"
	"github.com/tjcran/boardgame-go/modules/tabletop/regions"
)

type State struct {
	*ccg.State
	Space       *tabletop.State `json:"space"`
	Board       tabletop.Board  `json:"-"`
	Territories *regions.Map    `json:"territories"`
	Continents  *regions.Map    `json:"continents"`
	Moves       int             `json:"moves"`
}

const movesTotal = 8

func New() *core.Game {
	return &core.Game{
		Name:       "risklike",
		MinPlayers: 2, MaxPlayers: 2,
		Setup: setup,
		Moves: map[string]any{
			"place": core.MoveFn(place),
		},
		Turn:  &core.TurnConfig{MinMoves: 1, MaxMoves: 1},
		EndIf: endIf,
	}
}

func setup(_ core.Ctx, _ any) core.G {
	territories, _ := regions.NewMap([]regions.Region{
		{ID: "n_west", Cells: []tabletop.Pos{{X: 0, Y: 0}}},
		{ID: "n_mid", Cells: []tabletop.Pos{{X: 1, Y: 0}}},
		{ID: "n_east", Cells: []tabletop.Pos{{X: 2, Y: 0}}},
		{ID: "s_west", Cells: []tabletop.Pos{{X: 0, Y: 1}}},
		{ID: "s_mid", Cells: []tabletop.Pos{{X: 1, Y: 1}}},
		{ID: "s_east", Cells: []tabletop.Pos{{X: 2, Y: 1}}},
	})
	continents, _ := regions.NewMap([]regions.Region{
		{ID: "north", Cells: []tabletop.Pos{{X: 0, Y: 0}, {X: 1, Y: 0}, {X: 2, Y: 0}}},
		{ID: "south", Cells: []tabletop.Pos{{X: 0, Y: 1}, {X: 1, Y: 1}, {X: 2, Y: 1}}},
	})
	return &State{
		State:       ccg.NewState(),
		Space:       tabletop.NewState(),
		Board:       tabletop.NewSquareBoard(8, 8),
		Territories: territories,
		Continents:  continents,
	}
}

func place(mc *core.MoveContext, args ...any) (core.G, error) {
	g := mc.G.(*State)
	if mc.PlayerID != mc.Ctx.CurrentPlayer {
		return g, fmt.Errorf("%w: not your turn", core.ErrInvalidMove)
	}
	if len(args) < 2 {
		return g, fmt.Errorf("%w: need x, y", core.ErrInvalidMove)
	}
	x, ok1 := toIntAny(args[0])
	y, ok2 := toIntAny(args[1])
	if !ok1 || !ok2 {
		return g, fmt.Errorf("%w: x,y must be ints", core.ErrInvalidMove)
	}
	pos := tabletop.Pos{X: x, Y: y}
	if !g.Board.InBounds(pos) {
		return g, fmt.Errorf("%w: out of bounds", core.ErrInvalidMove)
	}
	if len(g.Space.EntitiesAt(pos)) > 0 {
		return g, fmt.Errorf("%w: cell occupied", core.ErrInvalidMove)
	}
	eid := g.NewEntity("unit", "", map[string]any{"owner": mc.PlayerID})
	g.Space.Place(tabletop.UnitID(eid), pos)
	g.Moves++
	return g, nil
}

func endIf(mc *core.MoveContext) any {
	g := mc.G.(*State)
	if g.Moves < movesTotal {
		return nil
	}
	owner := regions.ByCCGOwner(g.State)
	terr := g.Territories.ScoreAll(g.Space, owner, regions.ScoringRule{
		Kind:     regions.Plurality,
		PerPlace: []int{1},
		TieBreak: regions.NoAward,
	})
	cont := g.Continents.ScoreAll(g.Space, owner, regions.ScoringRule{
		Kind:      regions.Threshold,
		Threshold: 3,
		Payout:    3,
	})
	out := map[string]any{}
	for _, pid := range mc.Ctx.PlayOrder {
		out[pid] = terr[pid] + cont[pid]
	}
	return out
}

func toIntAny(v any) (int, bool) {
	switch x := v.(type) {
	case int:
		return x, true
	case int64:
		return int(x), true
	case float64:
		return int(x), true
	}
	return 0, false
}
