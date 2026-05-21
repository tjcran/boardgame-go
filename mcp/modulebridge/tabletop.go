package modulebridge

import (
	"fmt"

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

func asTabletop(state any) (*tabletopState, error) {
	s, ok := state.(*tabletopState)
	if !ok {
		return nil, fmt.Errorf("modulebridge: tabletop op got %T, want *tabletopState", state)
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
		Call: func(state any, args map[string]any) (any, error) {
			s, err := asTabletop(state)
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
		Call: func(state any, args map[string]any) (any, error) {
			s, err := asTabletop(state)
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

	return r
}
