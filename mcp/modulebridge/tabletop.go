package modulebridge

import (
	"fmt"

	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/modules/tabletop"
)

// tabletopState bundles the three live tabletop objects a spatial game
// needs. Board is nil until new_board runs; geometry ops error until then.
// JSON note: Space and Terrain marshal cleanly; Board is an interface and
// marshals as its concrete struct fields without a type tag — fine for the
// in-memory store (modules are not round-tripped between moves) and client
// views; replay rebuilds the board via setup's new_board call.
type tabletopState struct {
	Space   *tabletop.State      `json:"space"`
	Board   tabletop.Board       `json:"board,omitempty"`
	Terrain *tabletop.TerrainMap `json:"terrain"`
}

func newTabletopState() *tabletopState {
	return &tabletopState{
		Space:   tabletop.NewState(),
		Terrain: tabletop.NewTerrainMap(),
	}
}

func init() {
	stateFactories["tabletop"] = func() any { return newTabletopState() }
	registryByName["tabletop"] = TabletopRegistry()
}

var tabletopReg *Registry

// TabletopRegistry returns the registry holding the tabletop op set.
func TabletopRegistry() *Registry {
	if tabletopReg == nil {
		tabletopReg = buildTabletopRegistry()
	}
	return tabletopReg
}

func tabletopFrom(modules map[string]any) (*tabletopState, error) {
	s, ok := modules["tabletop"].(*tabletopState)
	if !ok {
		return nil, fmt.Errorf("modulebridge: tabletop op needs tabletop state, got %T", modules["tabletop"])
	}
	return s, nil
}

func pos(args map[string]any, xKey, yKey string) (tabletop.Pos, error) {
	x, err := argInt(args, xKey)
	if err != nil {
		return tabletop.Pos{}, err
	}
	y, err := argInt(args, yKey)
	if err != nil {
		return tabletop.Pos{}, err
	}
	return tabletop.Pos{X: x, Y: y}, nil
}

func posList(p tabletop.Pos) []any { return []any{int64(p.X), int64(p.Y)} }

func buildTabletopRegistry() *Registry {
	r := NewRegistry()

	r.Add(Op{Module: "tabletop", Name: "new_board", MCPTool: "tabletop_new_board",
		Call: func(modules map[string]any, args map[string]any, rng *core.Random) (any, error) {
			s, err := tabletopFrom(modules)
			if err != nil {
				return nil, err
			}
			kind, err := argStr(args, "kind")
			if err != nil {
				return nil, err
			}
			w, err := argInt(args, "w")
			if err != nil {
				return nil, err
			}
			h, err := argInt(args, "h")
			if err != nil {
				return nil, err
			}
			switch kind {
			case "square":
				s.Board = tabletop.NewSquareBoard(w, h)
			case "hex":
				s.Board = tabletop.NewHexBoard(w, h)
			default:
				return nil, fmt.Errorf("tabletop.new_board: unknown kind %q (want \"square\" or \"hex\")", kind)
			}
			return nil, nil
		}})

	r.Add(Op{Module: "tabletop", Name: "distance", MCPTool: "tabletop_distance",
		Call: func(modules map[string]any, args map[string]any, rng *core.Random) (any, error) {
			s, err := tabletopFrom(modules)
			if err != nil {
				return nil, err
			}
			if s.Board == nil {
				return nil, fmt.Errorf("tabletop.distance: no board; call new_board first")
			}
			a, err := pos(args, "ax", "ay")
			if err != nil {
				return nil, err
			}
			b, err := pos(args, "bx", "by")
			if err != nil {
				return nil, err
			}
			return int64(s.Board.Distance(a, b)), nil
		}})

	r.Add(Op{Module: "tabletop", Name: "place", MCPTool: "tabletop_place",
		Call: func(modules map[string]any, args map[string]any, rng *core.Random) (any, error) {
			s, err := tabletopFrom(modules)
			if err != nil {
				return nil, err
			}
			unit, err := argInt(args, "unit")
			if err != nil {
				return nil, err
			}
			p, err := pos(args, "x", "y")
			if err != nil {
				return nil, err
			}
			s.Space.Place(tabletop.UnitID(unit), p)
			return nil, nil
		}})

	r.Add(Op{Module: "tabletop", Name: "move", MCPTool: "tabletop_move",
		Call: func(modules map[string]any, args map[string]any, rng *core.Random) (any, error) {
			s, err := tabletopFrom(modules)
			if err != nil {
				return nil, err
			}
			unit, err := argInt(args, "unit")
			if err != nil {
				return nil, err
			}
			p, err := pos(args, "x", "y")
			if err != nil {
				return nil, err
			}
			s.Space.Move(tabletop.UnitID(unit), p)
			return nil, nil
		}})

	r.Add(Op{Module: "tabletop", Name: "remove", MCPTool: "tabletop_remove",
		Call: func(modules map[string]any, args map[string]any, rng *core.Random) (any, error) {
			s, err := tabletopFrom(modules)
			if err != nil {
				return nil, err
			}
			unit, err := argInt(args, "unit")
			if err != nil {
				return nil, err
			}
			s.Space.Remove(tabletop.UnitID(unit))
			return nil, nil
		}})

	r.Add(Op{Module: "tabletop", Name: "position_of", MCPTool: "tabletop_position_of",
		Call: func(modules map[string]any, args map[string]any, rng *core.Random) (any, error) {
			s, err := tabletopFrom(modules)
			if err != nil {
				return nil, err
			}
			unit, err := argInt(args, "unit")
			if err != nil {
				return nil, err
			}
			p, ok := s.Space.PositionOf(tabletop.UnitID(unit))
			if !ok {
				return nil, nil
			}
			return posList(p), nil
		}})

	r.Add(Op{Module: "tabletop", Name: "entities_at", MCPTool: "tabletop_entities_at",
		Call: func(modules map[string]any, args map[string]any, rng *core.Random) (any, error) {
			s, err := tabletopFrom(modules)
			if err != nil {
				return nil, err
			}
			p, err := pos(args, "x", "y")
			if err != nil {
				return nil, err
			}
			units := s.Space.EntitiesAt(p)
			out := make([]any, 0, len(units))
			for _, u := range units {
				out = append(out, int64(u))
			}
			return out, nil
		}})

	r.Add(Op{Module: "tabletop", Name: "within", MCPTool: "tabletop_within",
		Call: func(modules map[string]any, args map[string]any, rng *core.Random) (any, error) {
			s, err := tabletopFrom(modules)
			if err != nil {
				return nil, err
			}
			if s.Board == nil {
				return nil, fmt.Errorf("tabletop.within: no board; call new_board first")
			}
			center, err := pos(args, "x", "y")
			if err != nil {
				return nil, err
			}
			radius, err := argInt(args, "radius")
			if err != nil {
				return nil, err
			}
			units := s.Space.Within(s.Board, center, radius)
			out := make([]any, 0, len(units))
			for _, u := range units {
				out = append(out, int64(u))
			}
			return out, nil
		}})

	r.Add(Op{Module: "tabletop", Name: "neighbors", MCPTool: "tabletop_neighbors",
		Call: func(modules map[string]any, args map[string]any, rng *core.Random) (any, error) {
			s, err := tabletopFrom(modules)
			if err != nil {
				return nil, err
			}
			if s.Board == nil {
				return nil, fmt.Errorf("tabletop.neighbors: no board; call new_board first")
			}
			p, err := pos(args, "x", "y")
			if err != nil {
				return nil, err
			}
			cells := s.Board.Neighbors(p)
			out := make([]any, 0, len(cells))
			for _, c := range cells {
				out = append(out, posList(c))
			}
			return out, nil
		}})

	r.Add(Op{Module: "tabletop", Name: "line_of_sight", MCPTool: "tabletop_line_of_sight",
		Call: func(modules map[string]any, args map[string]any, rng *core.Random) (any, error) {
			s, err := tabletopFrom(modules)
			if err != nil {
				return nil, err
			}
			if s.Board == nil {
				return nil, fmt.Errorf("tabletop.line_of_sight: no board; call new_board first")
			}
			from, err := pos(args, "fx", "fy")
			if err != nil {
				return nil, err
			}
			target, err := pos(args, "tx", "ty")
			if err != nil {
				return nil, err
			}
			return tabletop.LineOfSight(s.Board, from, target, s.Terrain.Blocks), nil
		}})

	r.Add(Op{Module: "tabletop", Name: "tag_terrain", MCPTool: "tabletop_tag_terrain",
		Call: func(modules map[string]any, args map[string]any, rng *core.Random) (any, error) {
			s, err := tabletopFrom(modules)
			if err != nil {
				return nil, err
			}
			p, err := pos(args, "x", "y")
			if err != nil {
				return nil, err
			}
			tag, err := argStr(args, "tag")
			if err != nil {
				return nil, err
			}
			s.Terrain.Tag(p, tag)
			return nil, nil
		}})

	r.Add(Op{Module: "tabletop", Name: "has_terrain", MCPTool: "tabletop_has_terrain",
		Call: func(modules map[string]any, args map[string]any, rng *core.Random) (any, error) {
			s, err := tabletopFrom(modules)
			if err != nil {
				return nil, err
			}
			p, err := pos(args, "x", "y")
			if err != nil {
				return nil, err
			}
			tag, err := argStr(args, "tag")
			if err != nil {
				return nil, err
			}
			return s.Terrain.HasTag(p, tag), nil
		}})

	return r
}
