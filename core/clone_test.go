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
