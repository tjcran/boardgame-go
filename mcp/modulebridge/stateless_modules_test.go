package modulebridge

import (
	"encoding/json"
	"testing"
)

// economy and shop are stateless modules: their live state is an
// emptyState{} and all their persistent data (pools, shop stock) lives in
// the ccg state they operate on. So their own state round-trips trivially.
// This test pins that invariant — if either ever grows real state, it must
// gain a JSON codec and a rehydrator like ccg/tabletop did.
func TestStatelessModules_RoundTripIsTrivial(t *testing.T) {
	for _, name := range []string{"economy", "shop"} {
		st := NewState(name)
		if st == nil {
			t.Fatalf("%s: NewState returned nil", name)
		}
		if _, ok := st.(*emptyState); !ok {
			t.Fatalf("%s: live state is %T, expected *emptyState — if it now holds real state it needs a codec + rehydrator", name, st)
		}
		raw, err := json.Marshal(st)
		if err != nil {
			t.Fatalf("%s: marshal: %v", name, err)
		}
		fresh := NewState(name)
		if err := json.Unmarshal(raw, fresh); err != nil {
			t.Fatalf("%s: unmarshal: %v", name, err)
		}
	}
}
