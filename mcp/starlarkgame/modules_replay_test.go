package starlarkgame

import (
	"encoding/json"
	"testing"

	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/match"
	"github.com/tjcran/boardgame-go/storage"
)

func TestMiniCCG_DrawAcrossMovesAndReplay(t *testing.T) {
	spec, err := LoadSpec(miniCCGSpec)
	if err != nil {
		t.Fatalf("LoadSpec: %v", err)
	}
	g := BuildCoreGame(spec)

	mgr := match.NewManager(storage.NewMemory())
	mgr.MustRegister(g)

	id, err := mgr.Create("mini-ccg", match.CreateOptions{NumPlayers: 1})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	jr, err := mgr.Join(id, "p", match.JoinOptions{})
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	// Two draws: deck 3->1, hand 0->2. Proves the live *ccg.State and its
	// unexported counters persisted across moves (shared G pointer).
	for i := 0; i < 2; i++ {
		if _, err := mgr.MoveReq(id, jr.PlayerID, jr.PlayerCredentials, core.MoveRequest{Move: "draw"}); err != nil {
			t.Fatalf("move %d: %v", i, err)
		}
	}

	st, _ := mgr.State(id)
	sg := st.State.G.(*StarlarkG)
	ccgState := ccgStateOf(t, sg)
	if ccgState.Size("deck") != 1 || ccgState.Size("hand") != 2 {
		t.Fatalf("after 2 draws: deck=%d hand=%d, want 1/2", ccgState.Size("deck"), ccgState.Size("hand"))
	}

	// Replay the recorded log and assert byte-identical final G.
	replayed, err := core.Replay(g, st.State.Log, 1, nil)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	origJSON, _ := json.Marshal(st.State.G)
	replayJSON, _ := json.Marshal(replayed.G)
	if string(origJSON) != string(replayJSON) {
		t.Fatalf("replay mismatch:\n orig=%s\n repl=%s", origJSON, replayJSON)
	}
}
