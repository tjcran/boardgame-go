// Package threeprovinces is a 2-player area-control toy game built
// solely to validate the modules/tabletop/regions module surface.
//
// The board is a 4x4 SquareBoard partitioned into three regions:
// top (row 0), middle (row 1), bottom (rows 2-3). Players take turns
// placing one unit on an empty cell. After 8 placements (4 each) the
// game ends and regions are scored: plurality wins, 1 point per
// region, Split tie-break.
package threeprovinces

import (
	"fmt"

	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/modules/ccg"
	"github.com/tjcran/boardgame-go/modules/tabletop"
	"github.com/tjcran/boardgame-go/modules/tabletop/regions"
)

type State struct {
	*ccg.State
	Space     *tabletop.State `json:"space"`
	Board     tabletop.Board  `json:"-"`
	RegionMap *regions.Map    `json:"regionMap"`
	Moves     int             `json:"moves"`
}

const movesTotal = 8

func New() *core.Game {
	return &core.Game{
		Name:       "threeprovinces",
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
	rm, _ := regions.NewMap([]regions.Region{
		{ID: "top", Cells: []tabletop.Pos{{X: 0, Y: 0}, {X: 1, Y: 0}, {X: 2, Y: 0}, {X: 3, Y: 0}}},
		{ID: "middle", Cells: []tabletop.Pos{{X: 0, Y: 1}, {X: 1, Y: 1}, {X: 2, Y: 1}, {X: 3, Y: 1}}},
		{ID: "bottom", Cells: []tabletop.Pos{
			{X: 0, Y: 2}, {X: 1, Y: 2}, {X: 2, Y: 2}, {X: 3, Y: 2},
			{X: 0, Y: 3}, {X: 1, Y: 3}, {X: 2, Y: 3}, {X: 3, Y: 3},
		}},
	})
	return &State{
		State:     ccg.NewState(),
		Space:     tabletop.NewState(),
		Board:     tabletop.NewSquareBoard(4, 4),
		RegionMap: rm,
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
	pts := g.RegionMap.ScoreAll(
		g.Space,
		regions.ByCCGOwner(g.State),
		regions.ScoringRule{
			Kind:     regions.Plurality,
			PerPlace: []int{1},
			TieBreak: regions.Split,
		},
	)
	for _, pid := range mc.Ctx.PlayOrder {
		if _, ok := pts[pid]; !ok {
			pts[pid] = 0
		}
	}
	out := map[string]any{}
	for k, v := range pts {
		out[k] = v
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
