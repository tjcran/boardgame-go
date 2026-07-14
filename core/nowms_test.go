package core

import (
	"encoding/json"
	"testing"
)

// TestNowMsThreadsToHooksAndReplaysDeterministically pins the wall-clock
// contract: a request's NowMs is visible to moves AND turn hooks via
// ctx.NowMs, is recorded on the log entry, and Replay feeds the recorded
// value back so time-reading games replay identically.
func TestNowMsThreadsToHooksAndReplaysDeterministically(t *testing.T) {
	type gState struct {
		MoveSaw  int64 `json:"moveSaw"`
		BeginSaw int64 `json:"beginSaw"`
	}
	game := &Game{
		Name:       "nowms",
		MinPlayers: 2,
		MaxPlayers: 2,
		Setup:      func(_ Ctx, _ any) G { return &gState{} },
		DecodeG: func(raw json.RawMessage) (G, error) {
			var s gState
			if err := json.Unmarshal(raw, &s); err != nil {
				return nil, err
			}
			return &s, nil
		},
		Moves: map[string]any{
			"tick": MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
				s := mc.G.(*gState)
				s.MoveSaw = mc.Ctx.NowMs
				mc.Events.EndTurn("")
				return s, nil
			}),
		},
		Turn: &TurnConfig{
			MinMoves: 0, MaxMoves: 10,
			OnBegin: func(mc *MoveContext) G {
				s := mc.G.(*gState)
				s.BeginSaw = mc.Ctx.NowMs
				return s
			},
		},
	}

	state := NewMatch(game, 2, nil)
	const stamp = int64(1_784_000_000_123)
	next, err := Apply(game, state, MoveRequest{PlayerID: "0", Move: "tick", NowMs: stamp})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	s := next.G.(*gState)
	if s.MoveSaw != stamp {
		t.Fatalf("move saw NowMs=%d, want %d", s.MoveSaw, stamp)
	}
	// The endTurn queued by the move runs the NEXT turn's OnBegin within
	// the same apply — it must see the same request clock.
	if s.BeginSaw != stamp {
		t.Fatalf("turn OnBegin saw NowMs=%d, want %d", s.BeginSaw, stamp)
	}
	if got := next.Log[len(next.Log)-1].NowMs; got != stamp {
		t.Fatalf("log entry NowMs=%d, want %d", got, stamp)
	}

	// Replay from the log reproduces the same observed values.
	replayed, err := Replay(game, next.Log, 2, nil)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	rs := replayed.G.(*gState)
	if rs.MoveSaw != stamp || rs.BeginSaw != stamp {
		t.Fatalf("replay saw move=%d begin=%d, want %d", rs.MoveSaw, rs.BeginSaw, stamp)
	}
}
