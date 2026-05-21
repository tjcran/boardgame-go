package modulebridge

import (
	"sort"
	"testing"
)

func TestAllModules_IncludesBuiltinsSorted(t *testing.T) {
	got := AllModules()
	if !sort.StringsAreSorted(got) {
		t.Fatalf("AllModules() not sorted: %v", got)
	}
	have := map[string]bool{}
	for _, m := range got {
		have[m] = true
	}
	for _, want := range []string{"ccg", "tabletop", "economy", "shop"} {
		if !have[want] {
			t.Errorf("AllModules() missing %q; got %v", want, got)
		}
	}
}
