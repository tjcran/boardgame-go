package modulebridge

import (
	"encoding/json"
	"fmt"
)

var rehydrators = map[string]func(map[string]any) (any, error){}

// Rehydrate reconstructs a live typed module state from its JSON-decoded
// map form (as it comes back from a serializing match store). Returns an
// error for unknown modules.
func Rehydrate(name string, raw map[string]any) (any, error) {
	f := rehydrators[name]
	if f == nil {
		return nil, fmt.Errorf("modulebridge: no rehydrator for module %q", name)
	}
	return f(raw)
}

// jsonRehydrate re-marshals the decoded map and unmarshals it into a fresh
// typed state from the module's NewState factory, so the module's own JSON
// codec runs and produces a live typed value.
func jsonRehydrate(name string, raw map[string]any) (any, error) {
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	st := NewState(name)
	if st == nil {
		return nil, fmt.Errorf("modulebridge: no state factory for %q", name)
	}
	if err := json.Unmarshal(b, st); err != nil {
		return nil, fmt.Errorf("modulebridge: rehydrate %s: %w", name, err)
	}
	return st, nil
}
