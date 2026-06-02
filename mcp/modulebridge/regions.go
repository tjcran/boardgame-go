package modulebridge

import (
	"fmt"

	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/modules/ccg"
	"github.com/tjcran/boardgame-go/modules/tabletop"
	"github.com/tjcran/boardgame-go/modules/tabletop/regions"
)

// regionsState holds the live partition. JSON-safe via the embedded
// Map's tags. The lazy reverse index inside Map is rebuilt on first
// Of() call after rehydrate — no custom codec needed.
type regionsState struct {
	Map *regions.Map `json:"map,omitempty"`
}

func newRegionsState() *regionsState { return &regionsState{} }

func init() {
	stateFactories["regions"] = func() any { return newRegionsState() }
	registryByName["regions"] = RegionsRegistry()
	rehydrators["regions"] = func(m map[string]any) (any, error) {
		return jsonRehydrate("regions", m)
	}
}

var regionsReg *Registry

// RegionsRegistry returns the registry holding the regions op set.
func RegionsRegistry() *Registry {
	if regionsReg == nil {
		regionsReg = buildRegionsRegistry()
	}
	return regionsReg
}

func regionsFrom(modules map[string]any) (*regionsState, error) {
	s, ok := modules["regions"].(*regionsState)
	if !ok {
		return nil, fmt.Errorf("modulebridge: regions op needs regions state, got %T", modules["regions"])
	}
	return s, nil
}

func buildRegionsRegistry() *Registry {
	r := NewRegistry()

	r.Add(Op{Module: "regions", Name: "new_map", MCPTool: "regions_new_map",
		Call: func(modules map[string]any, args map[string]any, _ *core.Random) (any, error) {
			rs, err := regionsFrom(modules)
			if err != nil {
				return nil, err
			}
			rawRegs, ok := args["regions"].([]any)
			if !ok {
				return nil, fmt.Errorf("regions.new_map: 'regions' must be an array")
			}
			regs, err := decodeRegionsArgs(rawRegs)
			if err != nil {
				return nil, err
			}
			m, err := regions.NewMap(regs)
			if err != nil {
				return nil, err
			}
			rs.Map = m
			return map[string]any{"count": int64(len(regs))}, nil
		}})

	r.Add(Op{Module: "regions", Name: "of", MCPTool: "regions_of", ReadOnly: true,
		Call: func(modules map[string]any, args map[string]any, _ *core.Random) (any, error) {
			rs, err := regionsFrom(modules)
			if err != nil {
				return nil, err
			}
			if rs.Map == nil {
				return nil, fmt.Errorf("regions.of: no map; call new_map first")
			}
			p, err := pos(args, "x", "y")
			if err != nil {
				return nil, err
			}
			id, found := rs.Map.Of(p)
			return map[string]any{"region": string(id), "found": found}, nil
		}})

	r.Add(Op{Module: "regions", Name: "cells", MCPTool: "regions_cells", ReadOnly: true,
		Call: func(modules map[string]any, args map[string]any, _ *core.Random) (any, error) {
			rs, err := regionsFrom(modules)
			if err != nil {
				return nil, err
			}
			if rs.Map == nil {
				return nil, fmt.Errorf("regions.cells: no map; call new_map first")
			}
			id, err := argStr(args, "id")
			if err != nil {
				return nil, err
			}
			out := []any{}
			for _, p := range rs.Map.Cells(regions.RegionID(id)) {
				out = append(out, posList(p))
			}
			return out, nil
		}})

	r.Add(Op{Module: "regions", Name: "influence", MCPTool: "regions_influence", ReadOnly: true,
		Call: func(modules map[string]any, args map[string]any, _ *core.Random) (any, error) {
			rs, err := regionsFrom(modules)
			if err != nil {
				return nil, err
			}
			if rs.Map == nil {
				return nil, fmt.Errorf("regions.influence: no map; call new_map first")
			}
			ts, err := tabletopFrom(modules)
			if err != nil {
				return nil, err
			}
			cs, err := ccgFrom(modules)
			if err != nil {
				return nil, err
			}
			ownerAttr, _ := args["owner_attr"].(string)
			if ownerAttr == "" {
				ownerAttr = "owner"
			}
			owner := func(u tabletop.UnitID) string {
				e, ok := cs.Entities[ccg.EntityID(u)]
				if !ok {
					return ""
				}
				return e.AttrStr(ownerAttr, "")
			}
			infl := rs.Map.Influence(ts.Space, owner)
			out := map[string]any{}
			for id, per := range infl {
				inner := map[string]any{}
				for pid, n := range per {
					inner[pid] = int64(n)
				}
				out[string(id)] = inner
			}
			return out, nil
		}})

	r.Add(Op{Module: "regions", Name: "score_all", MCPTool: "regions_score_all", ReadOnly: true,
		Call: func(modules map[string]any, args map[string]any, _ *core.Random) (any, error) {
			rs, err := regionsFrom(modules)
			if err != nil {
				return nil, err
			}
			if rs.Map == nil {
				return nil, fmt.Errorf("regions.score_all: no map; call new_map first")
			}
			ts, err := tabletopFrom(modules)
			if err != nil {
				return nil, err
			}
			cs, err := ccgFrom(modules)
			if err != nil {
				return nil, err
			}
			ownerAttr, _ := args["owner_attr"].(string)
			if ownerAttr == "" {
				ownerAttr = "owner"
			}
			owner := func(u tabletop.UnitID) string {
				e, ok := cs.Entities[ccg.EntityID(u)]
				if !ok {
					return ""
				}
				return e.AttrStr(ownerAttr, "")
			}
			ruleRaw, _ := args["rule"].(map[string]any)
			rule, err := decodeRule(ruleRaw)
			if err != nil {
				return nil, err
			}
			pts := rs.Map.ScoreAll(ts.Space, owner, rule)
			out := map[string]any{}
			for pid, n := range pts {
				out[pid] = int64(n)
			}
			return out, nil
		}})

	return r
}

// decodeRegionsArgs converts the wire shape [{id, cells:[[x,y],...], label}]
// into []regions.Region.
func decodeRegionsArgs(raw []any) ([]regions.Region, error) {
	out := make([]regions.Region, 0, len(raw))
	for i, r := range raw {
		obj, ok := r.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("regions[%d]: not an object", i)
		}
		id, _ := obj["id"].(string)
		label, _ := obj["label"].(string)
		cellsRaw, _ := obj["cells"].([]any)
		cells := make([]tabletop.Pos, 0, len(cellsRaw))
		for j, c := range cellsRaw {
			pair, ok := c.([]any)
			if !ok || len(pair) != 2 {
				return nil, fmt.Errorf("regions[%d].cells[%d]: want [x,y]", i, j)
			}
			x, err := toIntCoord(pair[0])
			if err != nil {
				return nil, fmt.Errorf("regions[%d].cells[%d].x: %v", i, j, err)
			}
			y, err := toIntCoord(pair[1])
			if err != nil {
				return nil, fmt.Errorf("regions[%d].cells[%d].y: %v", i, j, err)
			}
			cells = append(cells, tabletop.Pos{X: x, Y: y})
		}
		out = append(out, regions.Region{
			ID:    regions.RegionID(id),
			Cells: cells,
			Label: label,
		})
	}
	return out, nil
}

func decodeRule(raw map[string]any) (regions.ScoringRule, error) {
	if raw == nil {
		return regions.ScoringRule{}, fmt.Errorf("rule: missing")
	}
	rule := regions.ScoringRule{}
	switch raw["kind"] {
	case "plurality":
		rule.Kind = regions.Plurality
	case "top_n", "topN":
		rule.Kind = regions.TopN
	case "threshold":
		rule.Kind = regions.Threshold
	default:
		return rule, fmt.Errorf("rule.kind: unknown %v", raw["kind"])
	}
	if pp, ok := raw["perPlace"].([]any); ok {
		for _, v := range pp {
			n, err := toIntCoord(v)
			if err != nil {
				return rule, fmt.Errorf("rule.perPlace: %v", err)
			}
			rule.PerPlace = append(rule.PerPlace, n)
		}
	}
	if v, ok := raw["threshold"]; ok {
		n, err := toIntCoord(v)
		if err != nil {
			return rule, fmt.Errorf("rule.threshold: %v", err)
		}
		rule.Threshold = n
	}
	if v, ok := raw["payout"]; ok {
		n, err := toIntCoord(v)
		if err != nil {
			return rule, fmt.Errorf("rule.payout: %v", err)
		}
		rule.Payout = n
	}
	switch raw["tieBreak"] {
	case "split", "", nil:
		rule.TieBreak = regions.Split
	case "no_award", "noAward":
		rule.TieBreak = regions.NoAward
	case "both_award", "bothAward":
		rule.TieBreak = regions.BothAward
	case "custom":
		return rule, fmt.Errorf("rule.tieBreak: 'custom' not supported over wire")
	default:
		return rule, fmt.Errorf("rule.tieBreak: unknown %v", raw["tieBreak"])
	}
	return rule, nil
}

func toIntCoord(v any) (int, error) {
	switch x := v.(type) {
	case int:
		return x, nil
	case int64:
		return int(x), nil
	case float64:
		return int(x), nil
	}
	return 0, fmt.Errorf("not a number: %T", v)
}
