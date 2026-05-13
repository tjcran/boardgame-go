package core

import "testing"

// loggerPlugin records every move and hook the engine calls. Lets us assert
// fnWrap reaches all the places it should.
type loggerPlugin struct{ calls *[]string }

func (l *loggerPlugin) Name() string { return "logger" }

func (l *loggerPlugin) WrapMove(inner MoveFn) MoveFn {
	return func(mc *MoveContext, args ...any) (G, error) {
		*l.calls = append(*l.calls, "move")
		return inner(mc, args...)
	}
}

func (l *loggerPlugin) WrapHook(inner HookFn, kind GameMethod) HookFn {
	return func(mc *MoveContext) G {
		*l.calls = append(*l.calls, hookName(kind))
		return inner(mc)
	}
}

func hookName(k GameMethod) string {
	switch k {
	case GameMethodTurnOnBegin:
		return "turnOnBegin"
	case GameMethodTurnOnEnd:
		return "turnOnEnd"
	case GameMethodTurnOnMove:
		return "turnOnMove"
	case GameMethodPhaseOnBegin:
		return "phaseOnBegin"
	case GameMethodPhaseOnEnd:
		return "phaseOnEnd"
	case GameMethodGameOnEnd:
		return "gameOnEnd"
	default:
		return "?"
	}
}

func TestFnWrapWrapsMovesAndHooks(t *testing.T) {
	var calls []string
	rec := func(name string) HookFn {
		return func(mc *MoveContext) G {
			// Sentinel value so we can tell the wrapper actually invoked
			// the inner — calls slice gets both "<kind>" and "<name>".
			calls = append(calls, name)
			return mc.G
		}
	}
	g := &Game{
		Name:       "fnwrap",
		MinPlayers: 1,
		MaxPlayers: 1,
		Setup:      func(_ Ctx, _ any) G { return struct{}{} },
		Plugins:    []Plugin{&loggerPlugin{calls: &calls}},
		Moves:      map[string]any{"x": noopMove},
		Turn: &TurnConfig{
			OnBegin:  rec("inner.turnOnBegin"),
			OnEnd:    rec("inner.turnOnEnd"),
			OnMove:   rec("inner.turnOnMove"),
			MinMoves: 1, MaxMoves: 1,
		},
	}
	s := NewMatch(g, 0, nil)
	if _, err := Apply(g, s, MoveRequest{PlayerID: "0", Move: "x"}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	// Expect at least: turnOnBegin (from NewMatch), move + turnOnMove +
	// turnOnEnd + turnOnBegin (from auto-end-turn) — each wrapper firing
	// before its inner.
	expectAdjacent(t, calls, "turnOnBegin", "inner.turnOnBegin")
	expectAdjacent(t, calls, "move", "")
	expectAdjacent(t, calls, "turnOnMove", "inner.turnOnMove")
}

// expectAdjacent asserts that wrapped appears in the calls slice
// immediately before innerCall (when innerCall is non-empty), to confirm
// the wrapper threaded through to the inner function. Empty innerCall
// just checks that the wrapped tag was recorded at all.
func expectAdjacent(t *testing.T, calls []string, wrapped, innerCall string) {
	t.Helper()
	for i, c := range calls {
		if c != wrapped {
			continue
		}
		if innerCall == "" {
			return
		}
		if i+1 < len(calls) && calls[i+1] == innerCall {
			return
		}
	}
	t.Fatalf("expected %q (followed by %q) in calls=%v", wrapped, innerCall, calls)
}
