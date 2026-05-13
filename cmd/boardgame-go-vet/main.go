// Command boardgame-go-vet runs the determinism analyzer over a package
// tree. Pluggable into go vet:
//
//	go install github.com/tjcran/boardgame-go/cmd/boardgame-go-vet@latest
//	go vet -vettool=$(which boardgame-go-vet) ./...
//
// Reports non-deterministic calls (time.Now, math/rand, etc.) inside
// MoveFn / HookFn / EndIfFn bodies. See package determinism for the
// full ban list.
package main

import (
	"golang.org/x/tools/go/analysis/unitchecker"

	"github.com/tjcran/boardgame-go/cmd/boardgame-go-vet/internal/determinism"
)

func main() { unitchecker.Main(determinism.Analyzer) }
