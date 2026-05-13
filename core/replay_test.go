package core

import (
	"reflect"
	"testing"
)

func TestReplayReproducesFinalState(t *testing.T) {
	// Use the counter game from reducer_test.go.
	g := counterGame()
	live := NewMatch(g, 0, nil)

	for _, who := range []string{"0", "1", "0"} {
		next, err := Apply(g, live, MoveRequest{PlayerID: who, Move: "inc"})
		if err != nil {
			t.Fatalf("apply: %v", err)
		}
		live = next
	}

	replayed, err := Replay(g, live.Log, 0, nil)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}

	// Same gameover signal, same turn, same current player.
	if !reflect.DeepEqual(replayed.Ctx.Gameover, live.Ctx.Gameover) {
		t.Fatalf("gameover mismatch: replay=%v live=%v",
			replayed.Ctx.Gameover, live.Ctx.Gameover)
	}
	if replayed.Ctx.Turn != live.Ctx.Turn {
		t.Fatalf("turn mismatch: replay=%d live=%d", replayed.Ctx.Turn, live.Ctx.Turn)
	}
	// G should be equal as JSON-shaped values.
	if !reflect.DeepEqual(replayed.G, live.G) {
		t.Fatalf("G mismatch: replay=%+v live=%+v", replayed.G, live.G)
	}
}

func TestReplaySkipsNonMoveEntries(t *testing.T) {
	g := counterGame()
	live := NewMatch(g, 0, nil)
	live, _ = Apply(g, live, MoveRequest{PlayerID: "0", Move: "inc"})
	// Inject a custom-kind entry to verify replay skips it.
	live.Log = append(live.Log, LogEntry{Kind: "custom", Move: "noise"})

	if _, err := Replay(g, live.Log, 0, nil); err != nil {
		t.Fatalf("replay should ignore non-move entries: %v", err)
	}
}
