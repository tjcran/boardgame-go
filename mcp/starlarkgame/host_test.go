package starlarkgame

import (
	"context"
	"testing"
)

func TestRunStarlarkExecutesSource(t *testing.T) {
	got, err := evalForTest(context.Background(), `result = 1 + 2`)
	if err != nil {
		t.Fatalf("evalForTest: %v", err)
	}
	if got["result"] != int64(3) {
		t.Fatalf("result: got %v, want 3", got["result"])
	}
}
