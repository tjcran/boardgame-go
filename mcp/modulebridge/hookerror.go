package modulebridge

// HookError wraps a hook-handler failure so it can cross the
// ccg.Publish callback boundary (subscribers are func(*State, Event)
// with no error return). The starlarkgame hook handler panics with a
// HookError on a Starlark error; the publish op recovers it into a
// normal error return, so a failing hook rejects the move.
type HookError struct{ Err error }

func (h HookError) Error() string {
	if h.Err == nil {
		return "modulebridge: hook handler failed"
	}
	return "hook handler: " + h.Err.Error()
}
