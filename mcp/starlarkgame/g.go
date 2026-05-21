package starlarkgame

import "encoding/json"

// StarlarkG is the game state (core.G) for designed games. Data is the
// user dict the spec's apply reads and returns. Modules holds live
// engine-module states ("ccg" -> *ccg.State, …) instantiated in Setup
// and carried in memory across moves, so their unexported ID counters
// persist exactly like a native Go game's G under the in-memory store.
//
// JSON contract: a module-free game marshals as the bare Data dict, so
// existing games and clients see the unchanged wire shape. When modules
// are present they are nested under the reserved "__modules__" key.
type StarlarkG struct {
	Data    map[string]any
	Modules map[string]any
}

const modulesKey = "__modules__"

func (g *StarlarkG) MarshalJSON() ([]byte, error) {
	if len(g.Modules) == 0 {
		if g.Data == nil {
			return []byte("{}"), nil
		}
		return json.Marshal(g.Data)
	}
	out := make(map[string]any, len(g.Data)+1)
	for k, v := range g.Data {
		out[k] = v
	}
	out[modulesKey] = g.Modules
	return json.Marshal(out)
}

func (g *StarlarkG) UnmarshalJSON(b []byte) error {
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	if mods, ok := raw[modulesKey]; ok {
		if m, ok := mods.(map[string]any); ok {
			g.Modules = m
		}
		delete(raw, modulesKey)
	}
	g.Data = raw
	return nil
}
