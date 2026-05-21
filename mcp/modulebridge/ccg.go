package modulebridge

import (
	"fmt"

	"github.com/tjcran/boardgame-go/modules/ccg"
)

func init() {
	stateFactories["ccg"] = func() any { return ccg.NewState() }
	registryByName["ccg"] = CCGRegistry()
}

var ccgReg *Registry

// CCGRegistry returns the registry holding the ccg op set, built once.
func CCGRegistry() *Registry {
	if ccgReg == nil {
		ccgReg = buildCCGRegistry()
	}
	return ccgReg
}

func ccgFrom(modules map[string]any) (*ccg.State, error) {
	s, ok := modules["ccg"].(*ccg.State)
	if !ok {
		return nil, fmt.Errorf("modulebridge: ccg op needs ccg state, got %T", modules["ccg"])
	}
	return s, nil
}

func argStr(args map[string]any, key string) (string, error) {
	v, ok := args[key]
	if !ok {
		return "", fmt.Errorf("missing arg %q", key)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("arg %q must be string, got %T", key, v)
	}
	return s, nil
}

func argBool(args map[string]any, key string) bool {
	b, _ := args[key].(bool)
	return b
}

func argInt(args map[string]any, key string) (int, error) {
	switch v := args[key].(type) {
	case int64:
		return int(v), nil
	case int:
		return v, nil
	case float64:
		return int(v), nil
	default:
		return 0, fmt.Errorf("arg %q must be int, got %T", key, args[key])
	}
}

func buildCCGRegistry() *Registry {
	r := NewRegistry()

	r.Add(Op{Module: "ccg", Name: "new_zone", MCPTool: "ccg_new_zone",
		Call: func(modules map[string]any, args map[string]any) (any, error) {
			s, err := ccgFrom(modules)
			if err != nil {
				return nil, err
			}
			name, err := argStr(args, "name")
			if err != nil {
				return nil, err
			}
			s.NewZone(ccg.ZoneName(name), argBool(args, "ordered"))
			return nil, nil
		}})

	r.Add(Op{Module: "ccg", Name: "new_entity", MCPTool: "ccg_new_entity",
		Call: func(modules map[string]any, args map[string]any) (any, error) {
			s, err := ccgFrom(modules)
			if err != nil {
				return nil, err
			}
			typ, err := argStr(args, "type")
			if err != nil {
				return nil, err
			}
			owner, _ := args["owner"].(string)
			var attrs map[string]any
			if a, ok := args["attrs"].(map[string]any); ok {
				attrs = a
			}
			id := s.NewEntity(typ, owner, attrs)
			return EntityToken(id), nil
		}})

	r.Add(Op{Module: "ccg", Name: "move_to", MCPTool: "ccg_move_to",
		Call: func(modules map[string]any, args map[string]any) (any, error) {
			s, err := ccgFrom(modules)
			if err != nil {
				return nil, err
			}
			tok, err := argStr(args, "entity")
			if err != nil {
				return nil, err
			}
			id, err := ParseEntityToken(tok)
			if err != nil {
				return nil, err
			}
			zone, err := argStr(args, "zone")
			if err != nil {
				return nil, err
			}
			return nil, s.MoveTo(id, ccg.ZoneName(zone))
		}})

	r.Add(Op{Module: "ccg", Name: "size", MCPTool: "ccg_size",
		Call: func(modules map[string]any, args map[string]any) (any, error) {
			s, err := ccgFrom(modules)
			if err != nil {
				return nil, err
			}
			zone, err := argStr(args, "zone")
			if err != nil {
				return nil, err
			}
			return int64(s.Size(ccg.ZoneName(zone))), nil
		}})

	r.Add(Op{Module: "ccg", Name: "members", MCPTool: "ccg_members",
		Call: func(modules map[string]any, args map[string]any) (any, error) {
			s, err := ccgFrom(modules)
			if err != nil {
				return nil, err
			}
			zone, err := argStr(args, "zone")
			if err != nil {
				return nil, err
			}
			z, ok := s.Zones[ccg.ZoneName(zone)]
			if !ok {
				return nil, ccg.ErrUnknownZone
			}
			out := make([]any, 0, len(z.Members))
			for _, m := range z.Members {
				out = append(out, EntityToken(m))
			}
			return out, nil
		}})

	r.Add(Op{Module: "ccg", Name: "draw", MCPTool: "ccg_draw",
		Call: func(modules map[string]any, args map[string]any) (any, error) {
			s, err := ccgFrom(modules)
			if err != nil {
				return nil, err
			}
			zone, err := argStr(args, "zone")
			if err != nil {
				return nil, err
			}
			n, err := argInt(args, "n")
			if err != nil {
				return nil, err
			}
			drawn, err := s.Draw(ccg.ZoneName(zone), n)
			if err != nil {
				return nil, err
			}
			out := make([]any, 0, len(drawn))
			for _, id := range drawn {
				out = append(out, EntityToken(id))
			}
			return out, nil
		}})

	r.Add(Op{Module: "ccg", Name: "publish", MCPTool: "ccg_publish",
		Call: func(modules map[string]any, args map[string]any) (res any, err error) {
			s, e := ccgFrom(modules)
			if e != nil {
				return nil, e
			}
			typ, e := argStr(args, "type")
			if e != nil {
				return nil, e
			}
			ev := ccg.Event{Type: typ}
			if tok, ok := args["source"].(string); ok && tok != "" {
				id, e := ParseEntityToken(tok)
				if e != nil {
					return nil, e
				}
				ev.Source = id
			}
			if tok, ok := args["target"].(string); ok && tok != "" {
				id, e := ParseEntityToken(tok)
				if e != nil {
					return nil, e
				}
				ev.Target = id
			}
			if d, ok := args["data"].(map[string]any); ok {
				ev.Data = d
			}
			defer func() {
				if r := recover(); r != nil {
					if he, ok := r.(HookError); ok {
						err = he
						return
					}
					panic(r)
				}
			}()
			s.Publish(ev)
			return nil, nil
		}})

	return r
}
