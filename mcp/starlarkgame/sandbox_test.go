package starlarkgame

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestSandboxBlocksLoad(t *testing.T) {
	_, err := evalSandbox(context.Background(), `load("foo.star", "bar")`, defaultLimits())
	if err == nil || !strings.Contains(err.Error(), "load") {
		t.Fatalf("expected load to be blocked, got %v", err)
	}
}

func TestSandboxEnforcesStepCap(t *testing.T) {
	// Infinite-ish loop; 1000-step cap should trip it fast.
	src := `
xs = []
for i in range(100000):
    xs.append(i)
`
	lim := defaultLimits()
	lim.MaxSteps = 1000
	_, err := evalSandbox(context.Background(), src, lim)
	if err == nil || !strings.Contains(err.Error(), "step") {
		t.Fatalf("expected step-cap error, got %v", err)
	}
}

func TestSandboxRespectsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	src := `
xs = []
for i in range(10000000):
    xs.append(i)
`
	start := time.Now()
	_, err := evalSandbox(ctx, src, defaultLimits())
	dur := time.Since(start)
	if err == nil {
		t.Fatalf("expected cancellation error")
	}
	if dur > 500*time.Millisecond {
		t.Fatalf("cancellation took too long: %v", dur)
	}
}
