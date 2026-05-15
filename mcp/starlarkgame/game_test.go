package starlarkgame

import (
	"testing"

	"github.com/tjcran/boardgame-go/core"
)

func TestBuildCoreGame_ReturnsRealGame(t *testing.T) {
	spec, err := LoadSpec(goodSpec)
	if err != nil { t.Fatalf("LoadSpec: %v", err) }
	g := BuildCoreGame(spec)
	if g.Name != "demo" {
		t.Errorf("Name = %q, want demo", g.Name)
	}
	if g.MinPlayers != 2 || g.MaxPlayers != 2 {
		t.Errorf("player bounds: %d-%d", g.MinPlayers, g.MaxPlayers)
	}
	if g.Setup == nil { t.Error("Setup nil") }
	if g.EndIf == nil { t.Error("EndIf nil") }
	if g.Moves == nil || len(g.Moves) == 0 { t.Error("Moves empty") }
	if g.Enumerate == nil { t.Error("Enumerate nil") }

	// Drive Setup once to confirm it returns a usable G.
	state := g.Setup(core.Ctx{NumPlayers: 2}, nil)
	if state == nil { t.Error("Setup returned nil G") }
}
