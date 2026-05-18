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

// CallMove invokes MOVES[moveName].apply(state, ctx, *args) and returns
// the new state from the function's return value. The input state is
// frozen, so mutation-style apply functions (write to state then
// implicitly return None) fail loudly instead of silently no-opping —
// the spec author switches to returning the new dict and the contract
// becomes unambiguous: apply is a pure transform from old state to new.
//
// To reject a move, the apply function calls fail(msg); the message
// propagates as the Go error.
func (s *Spec) CallMove(ctx context.Context, bc *BridgeCtx, moveName string, state map[string]any, args []any) (map[string]any, error) {
	mv, ok := s.Moves[moveName]
	if !ok {
		return nil, fmt.Errorf("unknown move %q", moveName)
	}

	stateSV, err := freezeState(state)
	if err != nil {
		return nil, err
	}

	sargs := starlark.Tuple{stateSV, bc.asStarlark()}
	for _, a := range args {
		sv, err := ToStarlark(a)
		if err != nil {
			return nil, err
		}
		sargs = append(sargs, sv)
	}
	res, err := starlark.Call(s.newThread(ctx), mv.Apply, sargs, nil)
	if err != nil {
		return nil, err
	}
	g, err := ToGo(res)
	if err != nil {
		return nil, fmt.Errorf("apply return: %w", err)
	}
	if g == nil {
		return nil, fmt.Errorf("apply for move %q returned None; it must return the new state dict (use fail(\"...\") to reject a move)", moveName)
	}
	out, ok := g.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("apply for move %q must return a state dict, got %T", moveName, g)
	}
	return out, nil
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

// freezeState converts state to a Starlark dict and freezes it (so the
// pure callee cannot mutate it). If a script mutates a frozen value,
// starlark-go raises and the error surfaces here.
func freezeState(state map[string]any) (starlark.Value, error) {
	sv, err := ToStarlark(state)
	if err != nil { return nil, err }
	sv.Freeze()
	return sv, nil
}

// CallEndIf invokes end_if(state, ctx). Returns the Go-converted result
// (typically map[string]any or nil).
func (s *Spec) CallEndIf(ctx context.Context, bc *BridgeCtx, state map[string]any) (any, error) {
	sv, err := freezeState(state)
	if err != nil { return nil, err }
	res, err := starlark.Call(s.newThread(ctx), s.EndIf, starlark.Tuple{sv, bc.asStarlark()}, nil)
	if err != nil { return nil, fmt.Errorf("end_if: %w", err) }
	return ToGo(res)
}

// CallLegalMoves invokes legal_moves(state, ctx). Expected return shape:
// list of {name: string, args: list}.
func (s *Spec) CallLegalMoves(ctx context.Context, bc *BridgeCtx, state map[string]any) ([]map[string]any, error) {
	sv, err := freezeState(state)
	if err != nil { return nil, err }
	res, err := starlark.Call(s.newThread(ctx), s.LegalMoves, starlark.Tuple{sv, bc.asStarlark()}, nil)
	if err != nil { return nil, fmt.Errorf("legal_moves: %w", err) }
	g, err := ToGo(res)
	if err != nil { return nil, err }
	lst, ok := g.([]any)
	if !ok { return nil, fmt.Errorf("legal_moves must return list, got %T", g) }
	out := make([]map[string]any, 0, len(lst))
	for _, e := range lst {
		m, ok := e.(map[string]any)
		if !ok { return nil, fmt.Errorf("legal_moves entry must be a dict, got %T", e) }
		out = append(out, m)
	}
	return out, nil
}

// CallPlayerView invokes player_view(state, player_id). When the spec
// omits player_view, returns state unchanged.
func (s *Spec) CallPlayerView(ctx context.Context, bc *BridgeCtx, state map[string]any, playerID string) (map[string]any, error) {
	if s.PlayerView == nil {
		return state, nil
	}
	sv, err := freezeState(state)
	if err != nil { return nil, err }
	res, err := starlark.Call(s.newThread(ctx), s.PlayerView, starlark.Tuple{sv, starlark.String(playerID)}, nil)
	if err != nil { return nil, fmt.Errorf("player_view: %w", err) }
	g, err := ToGo(res)
	if err != nil { return nil, err }
	out, ok := g.(map[string]any)
	if !ok { return nil, fmt.Errorf("player_view must return a dict, got %T", g) }
	return out, nil
}
