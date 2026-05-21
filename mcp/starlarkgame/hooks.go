package starlarkgame

import (
	"context"

	"github.com/tjcran/boardgame-go/mcp/modulebridge"
	"github.com/tjcran/boardgame-go/modules/ccg"
	"go.starlark.net/starlark"
)

// maxHookDepth bounds re-entrant hook cascades (a handler publishing an
// event that re-triggers handlers), mirroring core's chained-move cap.
const maxHookDepth = 32

// registerHooks subscribes each HOOKS handler on the live ccg state in
// mods. Handlers fire synchronously inside ccg.Publish; each invocation
// runs on a FRESH sandboxed Starlark thread (Starlark functions close
// over module globals, not a thread) and mutates the shared live module
// state via ctx.modules.*. A shared depth counter bounds recursion. A
// handler's Starlark error is raised as a modulebridge.HookError panic,
// which the ccg.publish op recovers into a move-rejecting error.
func registerHooks(s *Spec, mods map[string]any) {
	if len(s.Hooks) == 0 {
		return
	}
	cs, ok := mods["ccg"].(*ccg.State)
	if !ok {
		return
	}
	depth := 0
	for _, h := range s.Hooks {
		fn := h.Fn
		cs.Subscribe(ccg.MatchType(h.Type), func(_ *ccg.State, e ccg.Event) {
			if depth >= maxHookDepth {
				panic(modulebridge.HookError{Err: errHookDepth{}})
			}
			depth++
			defer func() { depth-- }()

			bc := &BridgeCtx{Modules: mods}
			sv, err := ToStarlark(eventToDict(e))
			if err != nil {
				panic(modulebridge.HookError{Err: err})
			}
			thread := s.newThread(context.Background())
			if _, err := starlark.Call(thread, fn, starlark.Tuple{sv, bc.asStarlark()}, nil); err != nil {
				panic(modulebridge.HookError{Err: err})
			}
		})
	}
}

type errHookDepth struct{}

func (errHookDepth) Error() string { return "hook recursion exceeded maxHookDepth" }

func eventToDict(e ccg.Event) map[string]any {
	out := map[string]any{"type": e.Type}
	if e.Source != 0 {
		out["source"] = modulebridge.EntityToken(e.Source)
	}
	if e.Target != 0 {
		out["target"] = modulebridge.EntityToken(e.Target)
	}
	if e.Data != nil {
		out["data"] = map[string]any(e.Data)
	}
	return out
}
