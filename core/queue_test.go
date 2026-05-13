package core

import (
	"errors"
	"testing"
)

// cascadeState is a tiny ledger for queue tests. Each move appends a
// string so we can assert ordering across the cascade.
type cascadeState struct{ Events []string }

func clone(c *cascadeState, more ...string) *cascadeState {
	out := &cascadeState{Events: append([]string(nil), c.Events...)}
	out.Events = append(out.Events, more...)
	return out
}

// TestQueuePushDrainsSynchronously verifies a move pushing to the queue
// triggers a follow-up move automatically.
func TestQueuePushDrainsSynchronously(t *testing.T) {
	g := &Game{
		Name:       "cascade",
		MinPlayers: 1,
		MaxPlayers: 1,
		Setup:      func(_ Ctx, _ any) G { return &cascadeState{} },
		Moves: map[string]any{
			"alpha": MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
				mc.Queue.Push(mc.PlayerID, "beta")
				return clone(mc.G.(*cascadeState), "alpha"), nil
			}),
			"beta": MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
				mc.Queue.Push(mc.PlayerID, "gamma")
				return clone(mc.G.(*cascadeState), "beta"), nil
			}),
			"gamma": MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
				return clone(mc.G.(*cascadeState), "gamma"), nil
			}),
		},
	}
	s := NewMatch(g, 0, nil)
	s, err := Apply(g, s, MoveRequest{PlayerID: "0", Move: "alpha"})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	got := s.G.(*cascadeState).Events
	want := []string{"alpha", "beta", "gamma"}
	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("expected cascade %v, got %v", want, got)
	}
	if len(s.Queue) != 0 {
		t.Fatalf("expected Queue empty after drain, got %v", s.Queue)
	}
}

// TestQueueBlockPausesCascade verifies a move that adds a block halts
// the drain; subsequent queued actions don't run until resume.
func TestQueueBlockPausesCascade(t *testing.T) {
	g := &Game{
		Name:       "pause-resume",
		MinPlayers: 1,
		MaxPlayers: 1,
		Setup:      func(_ Ctx, _ any) G { return &cascadeState{} },
		Moves: map[string]any{
			"alpha": MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
				mc.Queue.Push(mc.PlayerID, "askTarget")
				mc.Queue.Push(mc.PlayerID, "applyTarget")
				return clone(mc.G.(*cascadeState), "alpha"), nil
			}),
			"askTarget": MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
				mc.Queue.Block("select-target", mc.PlayerID, nil)
				return clone(mc.G.(*cascadeState), "askTarget"), nil
			}),
			"applyTarget": MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
				return clone(mc.G.(*cascadeState), "applyTarget"), nil
			}),
			// Resume move — what the client sends back with ResumeTag.
			"selectTarget": MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
				return clone(mc.G.(*cascadeState), "selectTarget"), nil
			}),
		},
	}
	s := NewMatch(g, 0, nil)
	s, err := Apply(g, s, MoveRequest{PlayerID: "0", Move: "alpha"})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	// alpha + askTarget ran; applyTarget is still queued; one block pending.
	got := s.G.(*cascadeState).Events
	wantSoFar := []string{"alpha", "askTarget"}
	if len(got) != 2 || got[0] != wantSoFar[0] || got[1] != wantSoFar[1] {
		t.Fatalf("expected pause after askTarget, got %v", got)
	}
	if len(s.Queue) != 1 || s.Queue[0].Move != "applyTarget" {
		t.Fatalf("expected applyTarget pending in queue, got %+v", s.Queue)
	}
	if len(s.Blocks) != 1 || s.Blocks[0].Tag != "select-target" {
		t.Fatalf("expected select-target block pending, got %+v", s.Blocks)
	}

	// Non-resume external move while blocked should be refused.
	_, err = Apply(g, s, MoveRequest{PlayerID: "0", Move: "alpha"})
	if !errors.Is(err, ErrBlocked) {
		t.Fatalf("expected ErrBlocked for non-resume move, got %v", err)
	}

	// Resume: provide the matching ResumeTag.
	s, err = Apply(g, s, MoveRequest{PlayerID: "0", Move: "selectTarget", ResumeTag: "select-target"})
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	got = s.G.(*cascadeState).Events
	want := []string{"alpha", "askTarget", "selectTarget", "applyTarget"}
	if len(got) != 4 {
		t.Fatalf("expected full cascade after resume, got %v", got)
	}
	for i, e := range want {
		if got[i] != e {
			t.Fatalf("cascade %d: got %s want %s (full=%v)", i, got[i], e, got)
		}
	}
	if len(s.Queue) != 0 || len(s.Blocks) != 0 {
		t.Fatalf("expected fully drained, got queue=%v blocks=%v", s.Queue, s.Blocks)
	}
}

// TestQueueUnknownResumeTagRejected ensures a bogus ResumeTag fails.
func TestQueueUnknownResumeTagRejected(t *testing.T) {
	g := &Game{
		Name:       "no-block",
		MinPlayers: 1, MaxPlayers: 1,
		Setup: func(_ Ctx, _ any) G { return &cascadeState{} },
		Moves: map[string]any{
			"x": noopMove,
		},
	}
	s := NewMatch(g, 0, nil)
	_, err := Apply(g, s, MoveRequest{PlayerID: "0", Move: "x", ResumeTag: "ghost"})
	if !errors.Is(err, ErrUnknownResumeTag) {
		t.Fatalf("expected ErrUnknownResumeTag, got %v", err)
	}
}

// TestQueueExternalMoveBumpsStateIDOnce verifies the cascade is atomic
// from a state-ID perspective — one external move, one ID bump,
// regardless of drain depth. Critical for OCC correctness.
func TestQueueExternalMoveBumpsStateIDOnce(t *testing.T) {
	g := &Game{
		Name:       "atomic-id",
		MinPlayers: 1, MaxPlayers: 1,
		Setup: func(_ Ctx, _ any) G { return &cascadeState{} },
		Moves: map[string]any{
			"chain": MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
				// Stack up 5 chained drain steps.
				for i := 0; i < 5; i++ {
					mc.Queue.Push(mc.PlayerID, "noop")
				}
				return clone(mc.G.(*cascadeState), "chain"), nil
			}),
			"noop": MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
				return clone(mc.G.(*cascadeState), "noop"), nil
			}),
		},
	}
	s := NewMatch(g, 0, nil)
	before := s.StateID
	s, err := Apply(g, s, MoveRequest{PlayerID: "0", Move: "chain"})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if s.StateID != before+1 {
		t.Fatalf("expected StateID to bump by 1 (atomic cascade), got %d -> %d",
			before, s.StateID)
	}
	// And 1 external + 5 drain entries in the log.
	external, drain := 0, 0
	for _, e := range s.Log {
		switch e.Kind {
		case "move":
			external++
		case "drain-step":
			drain++
		}
	}
	if external != 1 || drain != 5 {
		t.Fatalf("expected 1 move + 5 drain-step entries, got %d / %d", external, drain)
	}
}

// TestQueueOverflowRollsBack verifies hitting MaxDrainDepth restores
// the pre-Apply state.
func TestQueueOverflowRollsBack(t *testing.T) {
	g := &Game{
		Name:       "overflow",
		MinPlayers: 1, MaxPlayers: 1,
		Setup: func(_ Ctx, _ any) G { return &cascadeState{} },
		Moves: map[string]any{
			"loop": MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
				mc.Queue.Push(mc.PlayerID, "loop")
				return clone(mc.G.(*cascadeState), "loop"), nil
			}),
		},
	}
	s := NewMatch(g, 0, nil)
	preID := s.StateID
	preLog := len(s.Log)
	_, err := Apply(g, s, MoveRequest{PlayerID: "0", Move: "loop"})
	if !errors.Is(err, ErrDrainOverflow) {
		t.Fatalf("expected ErrDrainOverflow on infinite loop, got %v", err)
	}
	// The returned state should be the pre-Apply state (we rolled back).
	if s.StateID != preID || len(s.Log) != preLog {
		t.Fatalf("expected full rollback, got StateID=%d log=%d", s.StateID, len(s.Log))
	}
}

// TestQueueIgnoreBlocksMovePasses verifies a Move flagged IgnoreBlocks
// can run while State.Blocks is non-empty.
func TestQueueIgnoreBlocksMovePasses(t *testing.T) {
	g := &Game{
		Name:       "concede",
		MinPlayers: 1, MaxPlayers: 1,
		Setup: func(_ Ctx, _ any) G { return &cascadeState{} },
		Moves: map[string]any{
			"start": MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
				mc.Queue.Block("anything", mc.PlayerID, nil)
				return mc.G, nil
			}),
			"concede": Move{
				Move: func(mc *MoveContext, _ ...any) (G, error) {
					mc.Events.EndGame(map[string]any{"conceded": mc.PlayerID})
					return mc.G, nil
				},
				IgnoreBlocks: true,
			},
		},
	}
	s := NewMatch(g, 0, nil)
	s, _ = Apply(g, s, MoveRequest{PlayerID: "0", Move: "start"})
	if len(s.Blocks) != 1 {
		t.Fatal("setup: expected a pending block")
	}
	s, err := Apply(g, s, MoveRequest{PlayerID: "0", Move: "concede"})
	if err != nil {
		t.Fatalf("concede should bypass ErrBlocked, got %v", err)
	}
	if s.Ctx.Gameover == nil {
		t.Fatal("expected concede to end the game")
	}
}
