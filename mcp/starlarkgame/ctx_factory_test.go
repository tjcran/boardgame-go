package starlarkgame

import (
	"testing"

	"github.com/tjcran/boardgame-go/mcp/modulebridge"
)

func TestCtxFactories_EncodeContract(t *testing.T) {
	mods := map[string]any{"tabletop": modulebridge.NewState("tabletop")}

	r := NewReadCtx(2, "0", mods)
	if !r.ReadOnly {
		t.Error("NewReadCtx must produce a read-only ctx")
	}
	if r.Modules == nil {
		t.Error("NewReadCtx must wire the module map")
	}
	if r.NumPlayers != 2 || r.PlayerID != "0" {
		t.Errorf("NewReadCtx fields: np=%d pid=%q", r.NumPlayers, r.PlayerID)
	}

	w := NewWriteCtx(3, "1", mods)
	if w.ReadOnly {
		t.Error("NewWriteCtx must produce a mutable ctx")
	}
	if w.Modules == nil {
		t.Error("NewWriteCtx must wire the module map")
	}
	if w.NumPlayers != 3 || w.PlayerID != "1" {
		t.Errorf("NewWriteCtx fields: np=%d pid=%q", w.NumPlayers, w.PlayerID)
	}
}
