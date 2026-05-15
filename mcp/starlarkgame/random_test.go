package starlarkgame

import (
	"testing"
)

func TestCtxRandomIsDeterministic(t *testing.T) {
	src := `
def setup(ctx):
    return [ctx.random.range(100) for _ in range(5)]
`
	spec, err := compileSpecForTest(src)
	if err != nil { t.Fatalf("compile: %v", err) }

	bc1 := BridgeCtx{}
	bc1.AttachSeededRandom(1234)
	out1, _ := spec.callForTest("setup", []any{&bc1})

	bc2 := BridgeCtx{}
	bc2.AttachSeededRandom(1234)
	out2, _ := spec.callForTest("setup", []any{&bc2})

	if !equalAny(out1, out2) {
		t.Fatalf("same seed produced different sequences: %v vs %v", out1, out2)
	}

	bc3 := BridgeCtx{}
	bc3.AttachSeededRandom(9999)
	out3, _ := spec.callForTest("setup", []any{&bc3})
	if equalAny(out1, out3) {
		t.Fatalf("different seeds produced same sequence")
	}
}

func equalAny(a, b any) bool {
	la, lb := a.([]any), b.([]any)
	if len(la) != len(lb) { return false }
	for i := range la { if la[i] != lb[i] { return false } }
	return true
}
