package starlarkgame

import (
	"context"
	"errors"
	"fmt"

	"go.starlark.net/starlark"
)

// newThread builds a fresh, sandboxed Starlark thread for a single
// engine call. Each invocation (Setup, Apply, EndIf, …) uses its own
// thread so executions are isolated and bounded by the same step cap.
func (s *Spec) newThread(ctx context.Context) *starlark.Thread {
	t := &starlark.Thread{
		Name: "spec-call",
		Load: func(*starlark.Thread, string) (starlark.StringDict, error) {
			return nil, errors.New("load() is disabled in game specs")
		},
		Print: func(*starlark.Thread, string) {},
	}
	t.SetMaxExecutionSteps(uint64(defaultLimits().MaxSteps))
	t.SetLocal("ctx", ctx)
	return t
}

// CallSetup invokes setup(ctx) and returns the initial state as
// map[string]any. Returns an error if setup returns a non-dict value or
// the spec raises.
func (s *Spec) CallSetup(ctx context.Context, bc *BridgeCtx) (map[string]any, error) {
	res, err := starlark.Call(s.newThread(ctx), s.Setup, starlark.Tuple{bc.asStarlark()}, nil)
	if err != nil {
		return nil, fmt.Errorf("setup: %w", err)
	}
	g, err := ToGo(res)
	if err != nil {
		return nil, fmt.Errorf("setup: %w", err)
	}
	out, ok := g.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("setup must return a dict, got %T", g)
	}
	return out, nil
}
