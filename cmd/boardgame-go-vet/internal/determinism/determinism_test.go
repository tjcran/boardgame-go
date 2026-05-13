package determinism_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/tjcran/boardgame-go/cmd/boardgame-go-vet/internal/determinism"
)

// TestAnalyzer runs the analyser over testdata/src/example. The `// want`
// comments inside the package mark expected diagnostics; analysistest
// compares actual vs. expected and fails on drift.
func TestAnalyzer(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), determinism.Analyzer, "example")
}
