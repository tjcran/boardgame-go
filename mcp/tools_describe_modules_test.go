package mcp

import (
	"context"
	"sort"
	"testing"
)

func TestDescribeModules_AllModules(t *testing.T) {
	tools := &Tools{}
	res, err := tools.DescribeModules(context.Background(), DescribeModulesArgs{})
	if err != nil {
		t.Fatalf("DescribeModules: %v", err)
	}

	byName := map[string]ModuleInfo{}
	names := make([]string, 0, len(res.Modules))
	for _, m := range res.Modules {
		byName[m.Module] = m
		names = append(names, m.Module)
	}
	if !sort.StringsAreSorted(names) {
		t.Errorf("modules not sorted: %v", names)
	}
	for _, want := range []string{"ccg", "tabletop", "economy", "shop"} {
		if _, ok := byName[want]; !ok {
			t.Errorf("missing module %q in %v", want, names)
		}
	}

	ccg := byName["ccg"]
	if len(ccg.Ops) == 0 {
		t.Fatalf("ccg has no ops")
	}
	ro := map[string]bool{}
	seen := map[string]bool{}
	for _, op := range ccg.Ops {
		ro[op.Name] = op.ReadOnly
		seen[op.Name] = true
	}
	if !seen["new_zone"] {
		t.Errorf("ccg missing op new_zone; got %v", ccg.Ops)
	}
	if !ro["size"] {
		t.Errorf("ccg.size should report ReadOnly=true")
	}
	if ro["new_zone"] {
		t.Errorf("ccg.new_zone should report ReadOnly=false")
	}
}

func TestDescribeModules_FilterToOne(t *testing.T) {
	tools := &Tools{}
	res, err := tools.DescribeModules(context.Background(), DescribeModulesArgs{Module: "ccg"})
	if err != nil {
		t.Fatalf("DescribeModules: %v", err)
	}
	if len(res.Modules) != 1 || res.Modules[0].Module != "ccg" {
		t.Fatalf("filter returned %v, want only ccg", res.Modules)
	}
}

func TestDescribeModules_UnknownModuleErrors(t *testing.T) {
	tools := &Tools{}
	if _, err := tools.DescribeModules(context.Background(), DescribeModulesArgs{Module: "nope"}); err == nil {
		t.Fatalf("expected error for unknown module, got nil")
	}
}
