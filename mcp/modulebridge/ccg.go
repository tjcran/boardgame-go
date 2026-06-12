package modulebridge

import (
	"fmt"

	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/modules/ccg"
)

func init() {
	stateFactories["ccg"] = func() any { return ccg.NewState() }
	registryByName["ccg"] = CCGRegistry()
	rehydrators["ccg"] = func(m map[string]any) (any, error) { return jsonRehydrate("ccg", m) }
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
		Call: func(modules map[string]any, args map[string]any, rng *core.Random) (any, error) {
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
		Call: func(modules map[string]any, args map[string]any, rng *core.Random) (any, error) {
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
		Call: func(modules map[string]any, args map[string]any, rng *core.Random) (any, error) {
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

	r.Add(Op{Module: "ccg", Name: "size", MCPTool: "ccg_size", ReadOnly: true,
		Call: func(modules map[string]any, args map[string]any, rng *core.Random) (any, error) {
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

	r.Add(Op{Module: "ccg", Name: "members", MCPTool: "ccg_members", ReadOnly: true,
		Call: func(modules map[string]any, args map[string]any, rng *core.Random) (any, error) {
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
		Call: func(modules map[string]any, args map[string]any, rng *core.Random) (any, error) {
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
		Call: func(modules map[string]any, args map[string]any, rng *core.Random) (res any, err error) {
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

	r.Add(Op{Module: "ccg", Name: "shuffle", MCPTool: "ccg_shuffle",
		Call: func(modules map[string]any, args map[string]any, rng *core.Random) (any, error) {
			s, err := ccgFrom(modules)
			if err != nil {
				return nil, err
			}
			if rng == nil {
				return nil, fmt.Errorf("ccg.shuffle: no RNG available in this context")
			}
			zone, err := argStr(args, "zone")
			if err != nil {
				return nil, err
			}
			return nil, s.Shuffle(ccg.ZoneName(zone), rng)
		}})

	addCCGParityOps(r)
	return r
}

// argEntityID parses a required "ent:<n>" token arg.
func argEntityID(args map[string]any, key string) (ccg.EntityID, error) {
	tok, err := argStr(args, key)
	if err != nil {
		return 0, err
	}
	return ParseEntityToken(tok)
}

// optEntityID parses an optional entity token; zero when absent.
func optEntityID(args map[string]any, key string) (ccg.EntityID, error) {
	tok, ok := args[key].(string)
	if !ok || tok == "" {
		return 0, nil
	}
	return ParseEntityToken(tok)
}

// normalizeNum widens Go ints to int64 so op results look the same to
// Starlark / MCP callers regardless of which Go API produced them.
func normalizeNum(v any) any {
	if n, ok := v.(int); ok {
		return int64(n)
	}
	return v
}

// guardHooks runs fn and converts a HookError panic (raised by a
// Starlark subscriber misbehaving during a published event) into a
// returned error, the same way the publish op does.
func guardHooks(fn func() (any, error)) (res any, err error) {
	defer func() {
		if r := recover(); r != nil {
			if he, ok := r.(HookError); ok {
				err = he
				return
			}
			panic(r)
		}
	}()
	return fn()
}

// effectFromArgs assembles a ccg.Effect envelope from op args. ID is
// left for the push/replace call to assign or preserve.
func effectFromArgs(args map[string]any) (ccg.Effect, error) {
	kind, err := argStr(args, "kind")
	if err != nil {
		return ccg.Effect{}, err
	}
	eff := ccg.Effect{Kind: kind}
	if eff.Source, err = optEntityID(args, "source"); err != nil {
		return ccg.Effect{}, err
	}
	if eff.Target, err = optEntityID(args, "target"); err != nil {
		return ccg.Effect{}, err
	}
	if ctrl, ok := args["controller"].(string); ok {
		eff.Controller = ctrl
	}
	if tag, ok := args["tag"].(string); ok {
		eff.Tag = tag
	}
	if list, ok := args["targets"].([]any); ok {
		for _, item := range list {
			tok, ok := item.(string)
			if !ok {
				return ccg.Effect{}, fmt.Errorf("targets entries must be entity tokens, got %T", item)
			}
			id, err := ParseEntityToken(tok)
			if err != nil {
				return ccg.Effect{}, err
			}
			eff.Targets = append(eff.Targets, id)
		}
	}
	if d, ok := args["data"].(map[string]any); ok {
		eff.Data = d
	}
	return eff, nil
}

func effectToMap(e ccg.Effect) map[string]any {
	out := map[string]any{"id": EffectToken(e.ID), "kind": e.Kind}
	if e.Source != 0 {
		out["source"] = EntityToken(e.Source)
	}
	if e.Controller != "" {
		out["controller"] = e.Controller
	}
	if e.Target != 0 {
		out["target"] = EntityToken(e.Target)
	}
	if len(e.Targets) > 0 {
		toks := make([]any, 0, len(e.Targets))
		for _, t := range e.Targets {
			toks = append(toks, EntityToken(t))
		}
		out["targets"] = toks
	}
	if e.Data != nil {
		out["data"] = e.Data
	}
	if e.Countered {
		out["countered"] = true
	}
	if e.Tag != "" {
		out["tag"] = e.Tag
	}
	return out
}

var visibilityNames = map[string]ccg.Visibility{
	"public":     ccg.Public,
	"owner_only": ccg.OwnerOnly,
	"hidden":     ccg.Hidden,
}

// addCCGParityOps registers the modifier / counter / effect / entity /
// query ops that bring the bridge to parity with the Go ccg API.
// Catalog ops (register_def / instantiate) are deliberately absent:
// Catalog has no home inside module state yet — see the module state
// persistence plan.
func addCCGParityOps(r *Registry) {
	r.Add(Op{Module: "ccg", Name: "add_modifier", MCPTool: "ccg_add_modifier",
		Call: func(modules map[string]any, args map[string]any, rng *core.Random) (any, error) {
			s, err := ccgFrom(modules)
			if err != nil {
				return nil, err
			}
			target, err := argEntityID(args, "target")
			if err != nil {
				return nil, err
			}
			attr, err := argStr(args, "attribute")
			if err != nil {
				return nil, err
			}
			opName, err := argStr(args, "op")
			if err != nil {
				return nil, err
			}
			var mop ccg.ModifierOp
			switch opName {
			case "set":
				mop = ccg.OpSet
			case "add":
				mop = ccg.OpAdd
			case "mul":
				mop = ccg.OpMultiply
			default:
				return nil, fmt.Errorf("ccg.add_modifier: op must be set|add|mul, got %q", opName)
			}
			m := ccg.Modifier{Target: target, Attribute: attr, Op: mop, Value: args["value"]}
			if m.Source, err = optEntityID(args, "source"); err != nil {
				return nil, err
			}
			if m.ExpiresWith, err = optEntityID(args, "expires_with"); err != nil {
				return nil, err
			}
			if layer, ok := args["layer"]; ok {
				if m.Layer, err = argInt(map[string]any{"layer": layer}, "layer"); err != nil {
					return nil, err
				}
			}
			if note, ok := args["note"].(string); ok {
				m.Note = note
			}
			if zones, ok := args["while_in"].([]any); ok {
				for _, z := range zones {
					name, ok := z.(string)
					if !ok {
						return nil, fmt.Errorf("ccg.add_modifier: while_in entries must be zone names, got %T", z)
					}
					m.WhileIn = append(m.WhileIn, ccg.ZoneName(name))
				}
			}
			return ModifierToken(s.AddModifier(m)), nil
		}})

	r.Add(Op{Module: "ccg", Name: "remove_modifier", MCPTool: "ccg_remove_modifier",
		Call: func(modules map[string]any, args map[string]any, rng *core.Random) (any, error) {
			s, err := ccgFrom(modules)
			if err != nil {
				return nil, err
			}
			tok, err := argStr(args, "modifier")
			if err != nil {
				return nil, err
			}
			id, err := ParseModifierToken(tok)
			if err != nil {
				return nil, err
			}
			s.RemoveModifier(id)
			return nil, nil
		}})

	r.Add(Op{Module: "ccg", Name: "sweep_modifiers", MCPTool: "ccg_sweep_modifiers",
		Call: func(modules map[string]any, args map[string]any, rng *core.Random) (any, error) {
			s, err := ccgFrom(modules)
			if err != nil {
				return nil, err
			}
			removed := s.SweepModifiers()
			out := make([]any, 0, len(removed))
			for _, id := range removed {
				out = append(out, ModifierToken(id))
			}
			return out, nil
		}})

	r.Add(Op{Module: "ccg", Name: "effective_attr", MCPTool: "ccg_effective_attr", ReadOnly: true,
		Call: func(modules map[string]any, args map[string]any, rng *core.Random) (any, error) {
			s, err := ccgFrom(modules)
			if err != nil {
				return nil, err
			}
			id, err := argEntityID(args, "entity")
			if err != nil {
				return nil, err
			}
			attr, err := argStr(args, "attr")
			if err != nil {
				return nil, err
			}
			return normalizeNum(s.EffectiveAttr(id, attr, args["default"])), nil
		}})

	r.Add(Op{Module: "ccg", Name: "add_counter", MCPTool: "ccg_add_counter",
		Call: func(modules map[string]any, args map[string]any, rng *core.Random) (any, error) {
			s, err := ccgFrom(modules)
			if err != nil {
				return nil, err
			}
			id, err := argEntityID(args, "entity")
			if err != nil {
				return nil, err
			}
			kind, err := argStr(args, "kind")
			if err != nil {
				return nil, err
			}
			n, err := argInt(args, "n")
			if err != nil {
				return nil, err
			}
			return guardHooks(func() (any, error) {
				s.AddCounter(id, kind, n)
				return nil, nil
			})
		}})

	r.Add(Op{Module: "ccg", Name: "remove_counter", MCPTool: "ccg_remove_counter",
		Call: func(modules map[string]any, args map[string]any, rng *core.Random) (any, error) {
			s, err := ccgFrom(modules)
			if err != nil {
				return nil, err
			}
			id, err := argEntityID(args, "entity")
			if err != nil {
				return nil, err
			}
			kind, err := argStr(args, "kind")
			if err != nil {
				return nil, err
			}
			n, err := argInt(args, "n")
			if err != nil {
				return nil, err
			}
			return guardHooks(func() (any, error) {
				s.RemoveCounter(id, kind, n)
				return nil, nil
			})
		}})

	r.Add(Op{Module: "ccg", Name: "counters", MCPTool: "ccg_counters", ReadOnly: true,
		Call: func(modules map[string]any, args map[string]any, rng *core.Random) (any, error) {
			s, err := ccgFrom(modules)
			if err != nil {
				return nil, err
			}
			id, err := argEntityID(args, "entity")
			if err != nil {
				return nil, err
			}
			kind, err := argStr(args, "kind")
			if err != nil {
				return nil, err
			}
			return int64(s.Counters(id, kind)), nil
		}})

	r.Add(Op{Module: "ccg", Name: "push_effect", MCPTool: "ccg_push_effect",
		Call: func(modules map[string]any, args map[string]any, rng *core.Random) (any, error) {
			s, err := ccgFrom(modules)
			if err != nil {
				return nil, err
			}
			eff, err := effectFromArgs(args)
			if err != nil {
				return nil, err
			}
			return guardHooks(func() (any, error) {
				if argBool(args, "front") {
					return EffectToken(s.PushEffectFront(eff)), nil
				}
				return EffectToken(s.PushEffect(eff)), nil
			})
		}})

	r.Add(Op{Module: "ccg", Name: "remove_effect", MCPTool: "ccg_remove_effect",
		Call: func(modules map[string]any, args map[string]any, rng *core.Random) (any, error) {
			s, err := ccgFrom(modules)
			if err != nil {
				return nil, err
			}
			tok, err := argStr(args, "effect")
			if err != nil {
				return nil, err
			}
			id, err := ParseEffectToken(tok)
			if err != nil {
				return nil, err
			}
			return guardHooks(func() (any, error) {
				if !s.RemoveEffect(id) {
					return nil, fmt.Errorf("ccg.remove_effect: unknown effect %s", tok)
				}
				return nil, nil
			})
		}})

	r.Add(Op{Module: "ccg", Name: "replace_effect", MCPTool: "ccg_replace_effect",
		Call: func(modules map[string]any, args map[string]any, rng *core.Random) (any, error) {
			s, err := ccgFrom(modules)
			if err != nil {
				return nil, err
			}
			tok, err := argStr(args, "effect")
			if err != nil {
				return nil, err
			}
			id, err := ParseEffectToken(tok)
			if err != nil {
				return nil, err
			}
			next, err := effectFromArgs(args)
			if err != nil {
				return nil, err
			}
			return guardHooks(func() (any, error) {
				if !s.ReplaceEffect(id, next) {
					return nil, fmt.Errorf("ccg.replace_effect: unknown effect %s", tok)
				}
				return nil, nil
			})
		}})

	r.Add(Op{Module: "ccg", Name: "pending_effects", MCPTool: "ccg_pending_effects", ReadOnly: true,
		Call: func(modules map[string]any, args map[string]any, rng *core.Random) (any, error) {
			s, err := ccgFrom(modules)
			if err != nil {
				return nil, err
			}
			out := make([]any, 0, len(s.PendingEffects))
			for _, e := range s.PendingEffects {
				out = append(out, effectToMap(e))
			}
			return out, nil
		}})

	r.Add(Op{Module: "ccg", Name: "clone", MCPTool: "ccg_clone",
		Call: func(modules map[string]any, args map[string]any, rng *core.Random) (any, error) {
			s, err := ccgFrom(modules)
			if err != nil {
				return nil, err
			}
			id, err := argEntityID(args, "entity")
			if err != nil {
				return nil, err
			}
			owner, err := argStr(args, "owner")
			if err != nil {
				return nil, err
			}
			overrides, _ := args["overrides"].(map[string]any)
			nid, err := s.Clone(id, owner, overrides)
			if err != nil {
				return nil, err
			}
			return EntityToken(nid), nil
		}})

	r.Add(Op{Module: "ccg", Name: "set_controller", MCPTool: "ccg_set_controller",
		Call: func(modules map[string]any, args map[string]any, rng *core.Random) (any, error) {
			s, err := ccgFrom(modules)
			if err != nil {
				return nil, err
			}
			id, err := argEntityID(args, "entity")
			if err != nil {
				return nil, err
			}
			ctrl, _ := args["controller"].(string)
			s.SetController(id, ctrl)
			return nil, nil
		}})

	r.Add(Op{Module: "ccg", Name: "set_visibility", MCPTool: "ccg_set_visibility",
		Call: func(modules map[string]any, args map[string]any, rng *core.Random) (any, error) {
			s, err := ccgFrom(modules)
			if err != nil {
				return nil, err
			}
			id, err := argEntityID(args, "entity")
			if err != nil {
				return nil, err
			}
			name, err := argStr(args, "visibility")
			if err != nil {
				return nil, err
			}
			v, ok := visibilityNames[name]
			if !ok {
				return nil, fmt.Errorf("ccg.set_visibility: visibility must be public|owner_only|hidden, got %q", name)
			}
			s.SetVisibility(id, v)
			return nil, nil
		}})

	r.Add(Op{Module: "ccg", Name: "reveal_to", MCPTool: "ccg_reveal_to",
		Call: func(modules map[string]any, args map[string]any, rng *core.Random) (any, error) {
			s, err := ccgFrom(modules)
			if err != nil {
				return nil, err
			}
			id, err := argEntityID(args, "entity")
			if err != nil {
				return nil, err
			}
			list, ok := args["viewers"].([]any)
			if !ok {
				return nil, fmt.Errorf("ccg.reveal_to: viewers must be a list of player ids")
			}
			viewers := make([]string, 0, len(list))
			for _, v := range list {
				p, ok := v.(string)
				if !ok {
					return nil, fmt.Errorf("ccg.reveal_to: viewers entries must be strings, got %T", v)
				}
				viewers = append(viewers, p)
			}
			s.RevealTo(id, viewers...)
			return nil, nil
		}})

	r.Add(Op{Module: "ccg", Name: "conceal_from", MCPTool: "ccg_conceal_from",
		Call: func(modules map[string]any, args map[string]any, rng *core.Random) (any, error) {
			s, err := ccgFrom(modules)
			if err != nil {
				return nil, err
			}
			id, err := argEntityID(args, "entity")
			if err != nil {
				return nil, err
			}
			viewer, err := argStr(args, "viewer")
			if err != nil {
				return nil, err
			}
			s.ConcealFrom(id, viewer)
			return nil, nil
		}})

	r.Add(Op{Module: "ccg", Name: "set_mask", MCPTool: "ccg_set_mask",
		Call: func(modules map[string]any, args map[string]any, rng *core.Random) (any, error) {
			s, err := ccgFrom(modules)
			if err != nil {
				return nil, err
			}
			id, err := argEntityID(args, "entity")
			if err != nil {
				return nil, err
			}
			mask, _ := args["mask"].(map[string]any)
			s.SetMask(id, mask)
			return nil, nil
		}})

	r.Add(Op{Module: "ccg", Name: "query", MCPTool: "ccg_query", ReadOnly: true,
		Call: func(modules map[string]any, args map[string]any, rng *core.Random) (any, error) {
			s, err := ccgFrom(modules)
			if err != nil {
				return nil, err
			}
			q := ccg.Query(s)
			if zone, ok := args["zone"].(string); ok && zone != "" {
				q = q.InZone(ccg.ZoneName(zone))
			}
			if typ, ok := args["type"].(string); ok && typ != "" {
				q = q.HasType(typ)
			}
			if p, ok := args["controlled_by"].(string); ok && p != "" {
				q = q.Controlled(p)
			}
			if p, ok := args["not_controlled_by"].(string); ok && p != "" {
				q = q.NotControlled(p)
			}
			if p, ok := args["owned_by"].(string); ok && p != "" {
				q = q.OwnedBy(p)
			}
			for argKey, isMin := range map[string]bool{"attr_at_least": true, "attr_at_most": false} {
				spec, ok := args[argKey].(map[string]any)
				if !ok {
					continue
				}
				attr, err := argStr(spec, "attr")
				if err != nil {
					return nil, fmt.Errorf("ccg.query: %s needs attr: %w", argKey, err)
				}
				boundKey := "min"
				if !isMin {
					boundKey = "max"
				}
				bound, err := argInt(spec, boundKey)
				if err != nil {
					return nil, fmt.Errorf("ccg.query: %s needs %s: %w", argKey, boundKey, err)
				}
				if isMin {
					q = q.AttrAtLeast(attr, bound)
				} else {
					q = q.AttrAtMost(attr, bound)
				}
			}
			ids := q.Find()
			out := make([]any, 0, len(ids))
			for _, id := range ids {
				out = append(out, EntityToken(id))
			}
			return out, nil
		}})

	r.Add(Op{Module: "ccg", Name: "peek", MCPTool: "ccg_peek", ReadOnly: true,
		Call: func(modules map[string]any, args map[string]any, rng *core.Random) (any, error) {
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
			ids := s.Peek(ccg.ZoneName(zone), n)
			out := make([]any, 0, len(ids))
			for _, id := range ids {
				out = append(out, EntityToken(id))
			}
			return out, nil
		}})
}
