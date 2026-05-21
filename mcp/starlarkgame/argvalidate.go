package starlarkgame

import (
	"fmt"

	"github.com/tjcran/boardgame-go/mcp/modulebridge"
	"github.com/tjcran/boardgame-go/modules/ccg"
)

// validateArgs checks a move's actual args against its declared ArgsDef.
// A move with no ArgsDef is unconstrained. When ArgsDef is present, the
// arg count must match and each arg must satisfy its declared type.
// Semantic types (entity, hex) are validated against live module state.
func validateArgs(mv Move, args []any, modules map[string]any) error {
	if len(mv.ArgsDef) == 0 {
		return nil
	}
	if len(args) != len(mv.ArgsDef) {
		return fmt.Errorf("move %q expects %d args, got %d", mv.Name, len(mv.ArgsDef), len(args))
	}
	for i, ad := range mv.ArgsDef {
		if err := validateArg(ad, args[i], modules); err != nil {
			return fmt.Errorf("arg %q: %w", ad.Name, err)
		}
	}
	return nil
}

func validateArg(ad ArgDef, v any, modules map[string]any) error {
	switch ad.Type {
	case "", "any":
		return nil
	case "int":
		n, ok := v.(int64)
		if !ok {
			return fmt.Errorf("must be an int, got %T", v)
		}
		if ad.Min != nil && n < *ad.Min {
			return fmt.Errorf("must be >= %d", *ad.Min)
		}
		if ad.Max != nil && n > *ad.Max {
			return fmt.Errorf("must be <= %d", *ad.Max)
		}
		return nil
	case "string":
		if _, ok := v.(string); !ok {
			return fmt.Errorf("must be a string, got %T", v)
		}
		return nil
	case "bool":
		if _, ok := v.(bool); !ok {
			return fmt.Errorf("must be a bool, got %T", v)
		}
		return nil
	case "entity":
		tok, ok := v.(string)
		if !ok {
			return fmt.Errorf("must be an entity token string, got %T", v)
		}
		id, err := modulebridge.ParseEntityToken(tok)
		if err != nil {
			return err
		}
		if ad.Zone != "" {
			s, ok := modules["ccg"].(*ccg.State)
			if !ok {
				return fmt.Errorf("entity zone constraint needs the ccg module")
			}
			if !s.Contains(ccg.ZoneName(ad.Zone), id) {
				return fmt.Errorf("entity %s is not in zone %q", tok, ad.Zone)
			}
		}
		return nil
	case "hex":
		lst, ok := v.([]any)
		if !ok || len(lst) != 2 {
			return fmt.Errorf("must be a [x, y] pair")
		}
		for _, c := range lst {
			if _, ok := c.(int64); !ok {
				return fmt.Errorf("hex coordinates must be ints")
			}
		}
		return nil
	default:
		return fmt.Errorf("unknown arg type %q", ad.Type)
	}
}
