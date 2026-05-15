package starlarkgame

import (
	"context"
	"errors"
	"fmt"

	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
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

// Limits caps a single Starlark execution. Zero values mean "use default".
type Limits struct {
	MaxSteps int // hard cap on Starlark execution steps; 0 → 100_000.
}

func defaultLimits() Limits { return Limits{MaxSteps: 100_000} }

// evalSandbox runs src as a Starlark module in an isolated, deterministic
// thread. load() is disabled, print is silenced, and execution stops if
// ctx is cancelled or the step cap is reached.
func evalSandbox(ctx context.Context, src string, lim Limits) (map[string]any, error) {
	if lim.MaxSteps == 0 {
		lim.MaxSteps = defaultLimits().MaxSteps
	}
	thread := &starlark.Thread{
		Name: "spec",
		Load: func(*starlark.Thread, string) (starlark.StringDict, error) {
			return nil, errors.New("load() is disabled in game specs")
		},
		Print: func(*starlark.Thread, string) {},
	}
	thread.SetMaxExecutionSteps(uint64(lim.MaxSteps))
	thread.SetLocal("ctx", ctx)

	// Cancellation: cancel the Starlark thread when ctx is done.
	// We spawn a goroutine that signals the thread via Cancel; the
	// goroutine cleans up when evaluation finishes (via done channel).
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			thread.Cancel(ctx.Err().Error())
		case <-done:
		}
	}()

	// TopLevelControl allows if/for/while at top level, which game specs need.
	opts := &syntax.FileOptions{TopLevelControl: true}
	globals, err := starlark.ExecFileOptions(opts, thread, "spec.star", src, nil)
	if err != nil {
		return nil, err
	}
	out := make(map[string]any, len(globals))
	for k, v := range globals {
		out[k] = unwrap(v)
	}
	return out, nil
}

// Spec is a compiled game spec ready to be invoked. The full Spec type
// lands in spec.go (Task 6); this minimal stub exists so bridge_test.go
// can drive the bridge in isolation.
type Spec struct {
	globals starlark.StringDict
}

func compileSpecForTest(src string) (*Spec, error) {
	thread := &starlark.Thread{
		Name: "compile-test",
		Load: func(*starlark.Thread, string) (starlark.StringDict, error) {
			return nil, fmt.Errorf("load disabled")
		},
	}
	g, err := starlark.ExecFile(thread, "spec.star", src, nil)
	if err != nil {
		return nil, err
	}
	return &Spec{globals: g}, nil
}

func (s *Spec) callForTest(name string, args []any) (any, error) {
	fn, ok := s.globals[name]
	if !ok {
		return nil, fmt.Errorf("no global %q", name)
	}
	thread := &starlark.Thread{Name: "call-test"}
	sargs := make(starlark.Tuple, len(args))
	for i, a := range args {
		switch v := a.(type) {
		case BridgeCtx:
			sargs[i] = v.asStarlark()
		case *BridgeCtx:
			sargs[i] = v.asStarlark()
		default:
			sv, err := ToStarlark(a)
			if err != nil {
				return nil, err
			}
			sargs[i] = sv
		}
	}
	res, err := starlark.Call(thread, fn, sargs, nil)
	if err != nil {
		return nil, err
	}
	return ToGo(res)
}
