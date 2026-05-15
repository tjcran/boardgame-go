package starlarkgame

import (
	"context"

	"go.starlark.net/starlark"
)

// evalForTest compiles and executes a Starlark source string in a fresh
// thread, returning the resulting global bindings as a Go map. Used by
// host_test.go to exercise the bare-minimum interpreter wiring.
func evalForTest(ctx context.Context, src string) (map[string]any, error) {
	thread := &starlark.Thread{Name: "test"}
	globals, err := starlark.ExecFile(thread, "test.star", src, nil)
	if err != nil {
		return nil, err
	}
	out := make(map[string]any, len(globals))
	for k, v := range globals {
		out[k] = unwrap(v)
	}
	return out, nil
}

// unwrap converts a Starlark value to a Go value for the limited set of
// types evalForTest needs. The full converter lives in conv.go (Task 3).
func unwrap(v starlark.Value) any {
	if i, ok := v.(starlark.Int); ok {
		n, _ := i.Int64()
		return n
	}
	return v.String()
}
