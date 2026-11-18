package core

import "testing"

func TestCloneOwnsWhatTheReducerWritesInPlace(t *testing.T) {
	s := State{
		Ctx:        Ctx{ActivePlayers: map[string]string{"0": "play"}, PlayOrder: make([]string, 2, 4)},
		MoveCounts: map[string]int{"0": 1},
		Plugins:    map[string]any{"p": 1},
		Log:        make([]LogEntry, 1, 4),
	}
	c := s.Clone()

	s.Ctx.ActivePlayers["0"] = "changed"
	s.MoveCounts["0"] = 9
	s.Plugins["p"] = 9
	if c.Ctx.ActivePlayers["0"] != "play" || c.MoveCounts["0"] != 1 || c.Plugins["p"] != 1 {
		t.Fatalf("clone shares a map with the original: %+v", c)
	}

	// Appending to the clone must not write into the original's spare capacity.
	c.Log = append(c.Log, LogEntry{PlayerID: "x"})
	c.Ctx.PlayOrder = append(c.Ctx.PlayOrder, "x")
	if s.Log[:2][1].PlayerID == "x" || s.Ctx.PlayOrder[:3][2] == "x" {
		t.Fatal("append on the clone wrote into the original's backing array")
	}
}

func TestCloneKeepsNil(t *testing.T) {
	c := State{}.Clone()
	if c.Log != nil || c.Queue != nil || c.Ctx.ActivePlayers != nil || c.MoveCounts != nil {
		t.Fatalf("nil containers must stay nil: %+v", c)
	}
}

// A clone taken before an Undo must keep the log entry the undo drops:
// the next move appends to the shortened log, and without the capacity
// clip it would write into the backing array slot the clone still reads.
func TestCloneSurvivesUndoThenMove(t *testing.T) {
	g := multiMoveGame(false, true)
	s := NewMatch(g, 0, nil)
	for i := 0; i < 3; i++ {
		next, err := Apply(g, s, MoveRequest{PlayerID: "0", Move: "inc", Args: []any{i}})
		if err != nil {
			t.Fatalf("inc %d: %v", i, err)
		}
		s = next
	}
	// Give the live log spare capacity so the overwrite is possible
	// regardless of append's growth pattern.
	s.Log = append(make([]LogEntry, 0, 16), s.Log...)
	c := s.Clone()

	u, err := Undo(g, s)
	if err != nil {
		t.Fatalf("undo: %v", err)
	}
	if _, err := Apply(g, u, MoveRequest{PlayerID: "0", Move: "inc", Args: []any{"new"}}); err != nil {
		t.Fatalf("inc after undo: %v", err)
	}
	if got := c.Log[2].Args; len(got) != 1 || got[0] != 2 {
		t.Fatalf("clone's log entry was overwritten after undo: args=%v", got)
	}
}

// The undo snapshot's Queue can share its backing array with the state
// being undone. Once Undo makes the snapshot live, the next harvest
// appends to its Queue, which must not write into the undone state's
// entries.
func TestUndoDoesNotLetQueueAppendOverwriteUndoneState(t *testing.T) {
	g := multiMoveGame(false, true)
	backing := make([]QueuedAction, 2, 8)
	backing[0] = QueuedAction{Move: "a"}
	backing[1] = QueuedAction{Move: "kept"}
	snap := NewMatch(g, 0, nil)
	snap.Queue = backing[:1]
	undone := snap
	undone.Queue = backing[:2] // the undone state sees slot 1
	undone.Log = []LogEntry{{Move: "inc", PlayerID: "0"}}
	undone.TurnSnapshots = []State{snap}
	held := undone.Clone()

	u, err := Undo(g, undone)
	if err != nil {
		t.Fatalf("undo: %v", err)
	}
	u.Queue = append(u.Queue, QueuedAction{Move: "new"}) // what harvestQueue does
	if held.Queue[1].Move != "kept" || undone.Queue[1].Move != "kept" {
		t.Fatalf("append after undo overwrote the undone state's queue: %+v", held.Queue)
	}
}
