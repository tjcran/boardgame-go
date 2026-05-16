package starlarkgame

import (
	"fmt"

	"go.starlark.net/starlark"
)

// ToGo converts a Starlark value to a plain Go value built from
// primitives, []any, and map[string]any. Used to extract game state
// out of the Starlark thread and feed it back into core/match.
func ToGo(v starlark.Value) (any, error) {
	switch x := v.(type) {
	case starlark.NoneType:
		return nil, nil
	case starlark.Bool:
		return bool(x), nil
	case starlark.Int:
		i, ok := x.Int64()
		if !ok { return nil, fmt.Errorf("integer %v overflows int64", x) }
		return i, nil
	case starlark.Float:
		return float64(x), nil
	case starlark.String:
		return string(x), nil
	case *starlark.List:
		out := make([]any, 0, x.Len())
		iter := x.Iterate()
		defer iter.Done()
		var item starlark.Value
		for iter.Next(&item) {
			g, err := ToGo(item)
			if err != nil { return nil, err }
			out = append(out, g)
		}
		return out, nil
	case starlark.Tuple:
		out := make([]any, 0, x.Len())
		for i := 0; i < x.Len(); i++ {
			g, err := ToGo(x.Index(i))
			if err != nil { return nil, err }
			out = append(out, g)
		}
		return out, nil
	case *starlark.Dict:
		out := make(map[string]any, x.Len())
		for _, k := range x.Keys() {
			ks, ok := k.(starlark.String)
			if !ok { return nil, fmt.Errorf("dict key %v is not a string", k) }
			val, _, err := x.Get(k)
			if err != nil { return nil, err }
			g, err := ToGo(val)
			if err != nil { return nil, err }
			out[string(ks)] = g
		}
		return out, nil
	}
	return nil, fmt.Errorf("unsupported starlark type: %T", v)
}

// ToStarlark converts a Go value (typically produced by ToGo or by JSON
// decoding) back into a Starlark value. Inverse of ToGo for the supported
// shape: nil, bool, int/int64/float64, string, []any, map[string]any.
func ToStarlark(v any) (starlark.Value, error) {
	switch x := v.(type) {
	case nil:
		return starlark.None, nil
	case bool:
		return starlark.Bool(x), nil
	case int:
		return starlark.MakeInt(x), nil
	case int64:
		return starlark.MakeInt64(x), nil
	case float64:
		return starlark.Float(x), nil
	case string:
		return starlark.String(x), nil
	case []any:
		items := make([]starlark.Value, 0, len(x))
		for _, e := range x {
			sv, err := ToStarlark(e)
			if err != nil { return nil, err }
			items = append(items, sv)
		}
		return starlark.NewList(items), nil
	case map[string]any:
		d := starlark.NewDict(len(x))
		for k, vv := range x {
			sv, err := ToStarlark(vv)
			if err != nil { return nil, err }
			if err := d.SetKey(starlark.String(k), sv); err != nil {
				return nil, err
			}
		}
		return d, nil
	}
	return nil, fmt.Errorf("unsupported go type: %T", v)
}
