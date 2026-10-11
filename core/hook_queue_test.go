package core

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// hookQState records, in order, what moves and hooks did, so tests can
// assert both that something ran and where it ran relative to the turn
// transition.
type hookQState struct{ Log []string }

func hookQAppend(g G, entry string) *hookQState {
	prev := g.(*hookQState)
	out := &hookQState{Log: append([]string(nil), prev.Log...)}
	out.Log = append(out.Log, entry)
	return out
}

var errHookQFail = errors.New("hook queue test: step failed")

// hookQueueGame builds a two-player game whose Turn.OnBegin and
// Turn.OnEnd record themselves and then call the supplied functions, so
// each test decides what a hook pushes or raises on mc.Queue.
//
//   - "end" ends the turn.
//   - "noop" is an unrelated move, used to show non-resume moves stay
//     gated while a block is pending.
//   - "pick" is a resume move; it fails unless it is resuming a block.
//   - "draw" records who drew and who held the turn when it ran.
//   - "fail" always errors, for the cascade-failure tests.
//   - "ask" raises a block for the caller (a prompt left stranded).
//   - "forceTimeout" is the timeout escape hatch: drop the current
//     player's pending blocks and end the turn.
//   - "chain" runs "chainedBlock" via Events.RunMove, which raises a block.
func hookQueueGame(onBegin, onEnd func(mc *MoveContext)) *Game {
	return &Game{
		Name:       "hook-queue",
		MinPlayers: 2,
		MaxPlayers: 2,
		Setup:      func(_ Ctx, _ any) G { return &hookQState{} },
		Moves: map[string]any{
			"end": MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
				mc.Events.EndTurn()
				return hookQAppend(mc.G, "end:"+mc.PlayerID), nil
			}),
			"noop": MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
				return hookQAppend(mc.G, "noop:"+mc.PlayerID), nil
			}),
			"pick": MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
				if mc.ResumingBlock == nil {
					return mc.G, errors.New("pick must resume a block")
				}
				return hookQAppend(mc.G, "pick:"+mc.PlayerID+":"+mc.ResumingBlock.Tag), nil
			}),
			"draw": MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
				return hookQAppend(mc.G, "draw:"+mc.PlayerID+"@"+mc.Ctx.CurrentPlayer), nil
			}),
			"fail": MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
				return mc.G, errHookQFail
			}),
			"ask": MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
				mc.Queue.Block("stale", mc.PlayerID, nil)
				return hookQAppend(mc.G, "ask:"+mc.PlayerID), nil
			}),
			"forceTimeout": Move{
				AnyPlayer:    true,
				IgnoreBlocks: true,
				Move: MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
					mc.DropBlocksFor(mc.Ctx.CurrentPlayer)
					mc.Events.EndTurn()
					return hookQAppend(mc.G, "timeout:"+mc.Ctx.CurrentPlayer), nil
				}),
			},
			"chain": MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
				mc.Events.RunMove("chainedBlock")
				return hookQAppend(mc.G, "chain:"+mc.PlayerID), nil
			}),
			"chainedBlock": MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
				mc.Queue.Block("chained", mc.PlayerID, nil)
				return hookQAppend(mc.G, "chained:"+mc.PlayerID), nil
			}),
		},
		Turn: &TurnConfig{
			OnBegin: func(mc *MoveContext) G {
				g := hookQAppend(mc.G, fmt.Sprintf("begin:%s@%d", mc.Ctx.CurrentPlayer, mc.Ctx.Turn))
				if onBegin != nil {
					onBegin(mc)
				}
				return g
			},
			OnEnd: func(mc *MoveContext) G {
				g := hookQAppend(mc.G, fmt.Sprintf("turnEnd:%s@%d", mc.Ctx.CurrentPlayer, mc.Ctx.Turn))
				if onEnd != nil {
					onEnd(mc)
				}
				return g
			},
		},
	}
}

func hookQLog(s State) []string { return s.G.(*hookQState).Log }

// indexOf returns the position of entry in log, or -1.
func indexOf(log []string, entry string) int {
	for i, e := range log {
		if e == entry {
			return i
		}
	}
	return -1
}

// startPrompt is the start-of-turn prompt the OnBegin tests raise.
func startPrompt(mc *MoveContext, addressee string) {
	mc.Queue.RequestTarget(addressee, TargetRequest{
		Kind: "start-pick", Candidates: []any{1, 2}, Min: 1, Max: 1, Source: "turn-start",
	}, AnsweredBy("pick"))
}

// TestTurnOnBeginBlockAfterEndTurn: a Turn.OnBegin that raises a target
// prompt during a move-driven EndTurn leaves it in State.Blocks, addressed
// to the player it named. Non-resume moves are refused with ErrBlocked
// while it is open, and the addressee answers it with the matching
// ResumeTag, out of turn when the turn is not theirs.
func TestTurnOnBeginBlockAfterEndTurn(t *testing.T) {
	cases := []struct {
		name      string
		addressee string
	}{
		{name: "new current player", addressee: "1"},
		{name: "off-turn player", addressee: "0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			game := hookQueueGame(func(mc *MoveContext) {
				if mc.Ctx.Turn == 2 {
					startPrompt(mc, tc.addressee)
				}
			}, nil)
			state := NewMatch(game, 2, nil)
			if len(state.Blocks) != 0 {
				t.Fatalf("setup blocks = %v, want none (prompt is for turn 2)", state.Blocks)
			}

			state, err := Apply(game, state, MoveRequest{PlayerID: "0", Move: "end"})
			if err != nil {
				t.Fatalf("end: %v", err)
			}
			if state.Ctx.CurrentPlayer != "1" || state.Ctx.Turn != 2 {
				t.Fatalf("turn did not pass: current=%s turn=%d", state.Ctx.CurrentPlayer, state.Ctx.Turn)
			}
			if len(state.Blocks) != 1 {
				t.Fatalf("blocks = %v, want the OnBegin prompt", state.Blocks)
			}
			b := state.Blocks[0]
			if b.Tag != "start-pick" || b.PlayerID != tc.addressee || b.Move != "pick" || b.Target == nil {
				t.Fatalf("block = %+v, want start-pick for %s answered by pick", b, tc.addressee)
			}

			// Non-resume moves are gated, whoever sends them.
			for _, pid := range []string{"0", "1"} {
				if _, err := Apply(game, state, MoveRequest{PlayerID: pid, Move: "noop"}); !errors.Is(err, ErrBlocked) {
					t.Fatalf("noop by %s err = %v, want ErrBlocked", pid, err)
				}
			}
			if _, err := Apply(game, state, MoveRequest{PlayerID: "1", Move: "end"}); !errors.Is(err, ErrBlocked) {
				t.Fatalf("end while blocked err = %v, want ErrBlocked", err)
			}

			state, err = Apply(game, state, MoveRequest{
				PlayerID: tc.addressee, Move: "pick", Args: []any{1}, ResumeTag: "start-pick",
			})
			if err != nil {
				t.Fatalf("addressee %s could not answer the start-of-turn prompt: %v", tc.addressee, err)
			}
			if len(state.Blocks) != 0 {
				t.Fatalf("blocks after answer = %v, want none", state.Blocks)
			}
			if want := "pick:" + tc.addressee + ":start-pick"; indexOf(hookQLog(state), want) < 0 {
				t.Fatalf("log %v missing %q", hookQLog(state), want)
			}
			if state.Ctx.CurrentPlayer != "1" {
				t.Fatalf("answering a prompt moved the turn: current=%s", state.Ctx.CurrentPlayer)
			}
		})
	}
}

// TestTurnOnBeginPushRunsAfterTransition: a move Turn.OnBegin pushes runs
// in the same Apply, after the turn transition, so it sees the new
// CurrentPlayer. It is logged as a drain step of the move that ended the
// turn.
func TestTurnOnBeginPushRunsAfterTransition(t *testing.T) {
	game := hookQueueGame(func(mc *MoveContext) {
		if mc.Ctx.Turn == 2 {
			mc.Queue.Push(mc.Ctx.CurrentPlayer, "draw")
		}
	}, nil)
	state := NewMatch(game, 2, nil)
	state, err := Apply(game, state, MoveRequest{PlayerID: "0", Move: "end"})
	if err != nil {
		t.Fatalf("end: %v", err)
	}
	want := []string{"begin:0@1", "end:0", "turnEnd:0@1", "begin:1@2", "draw:1@1"}
	if got := hookQLog(state); !reflect.DeepEqual(got, want) {
		t.Fatalf("log = %v, want %v", got, want)
	}
	if len(state.Queue) != 0 || len(state.Blocks) != 0 {
		t.Fatalf("queue=%v blocks=%v, want both empty", state.Queue, state.Blocks)
	}
	parent := -1
	for i, e := range state.Log {
		if e.Kind == "move" && e.Move == "end" {
			parent = i
		}
		if e.Kind == "drain-step" && e.Move == "draw" {
			if parent < 0 || e.Parent != parent {
				t.Fatalf("draw drain step Parent = %d, want %d (the end move)", e.Parent, parent)
			}
			return
		}
	}
	t.Fatalf("no drain-step entry for draw in %+v", state.Log)
}

// TestHookPushesFollowTheMovesOwn: the move's own Queue still works, and
// what a hook pushes is harvested after it, in order.
func TestHookPushesFollowTheMovesOwn(t *testing.T) {
	game := hookQueueGame(func(mc *MoveContext) {
		if mc.Ctx.Turn == 2 {
			mc.Queue.Push(mc.Ctx.CurrentPlayer, "draw")
		}
	}, nil)
	game.Moves["drawThenEnd"] = MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
		mc.Queue.Push(mc.PlayerID, "draw")
		mc.Events.EndTurn()
		return hookQAppend(mc.G, "drawThenEnd:"+mc.PlayerID), nil
	})
	state := NewMatch(game, 2, nil)
	state, err := Apply(game, state, MoveRequest{PlayerID: "0", Move: "drawThenEnd"})
	if err != nil {
		t.Fatalf("drawThenEnd: %v", err)
	}
	// Both pushes run after the transition (the cascade drains after the
	// event drain), the move's first.
	want := []string{"begin:0@1", "drawThenEnd:0", "turnEnd:0@1", "begin:1@2", "draw:0@1", "draw:1@1"}
	if got := hookQLog(state); !reflect.DeepEqual(got, want) {
		t.Fatalf("log = %v, want %v", got, want)
	}
}

// TestTurnOnEndBlockDoesNotSuspendRotation pins the documented semantic:
// a block raised in Turn.OnEnd does not hold the turn open. The turn
// passes and the next turn's OnBegin runs in the same drain; the block
// then pauses the cascade, so a move the new turn's OnBegin pushed waits
// until the block is answered.
func TestTurnOnEndBlockDoesNotSuspendRotation(t *testing.T) {
	game := hookQueueGame(
		func(mc *MoveContext) {
			if mc.Ctx.Turn == 2 {
				mc.Queue.Push(mc.Ctx.CurrentPlayer, "draw")
			}
		},
		func(mc *MoveContext) {
			if mc.Ctx.Turn == 1 {
				mc.Queue.Block("end-confirm", mc.Ctx.CurrentPlayer, nil, AnsweredBy("pick"))
			}
		},
	)
	state := NewMatch(game, 2, nil)
	state, err := Apply(game, state, MoveRequest{PlayerID: "0", Move: "end"})
	if err != nil {
		t.Fatalf("end: %v", err)
	}
	if state.Ctx.CurrentPlayer != "1" || state.Ctx.Turn != 2 {
		t.Fatalf("OnEnd block held the turn: current=%s turn=%d", state.Ctx.CurrentPlayer, state.Ctx.Turn)
	}
	log := hookQLog(state)
	if indexOf(log, "turnEnd:0@1") < 0 || indexOf(log, "begin:1@2") < indexOf(log, "turnEnd:0@1") {
		t.Fatalf("log = %v, want the next turn's OnBegin after OnEnd", log)
	}
	if len(state.Blocks) != 1 || state.Blocks[0].Tag != "end-confirm" || state.Blocks[0].PlayerID != "0" {
		t.Fatalf("blocks = %+v, want the OnEnd block for 0", state.Blocks)
	}
	// The block pauses the cascade: OnBegin's push has not run.
	if indexOf(log, "draw:1@1") >= 0 {
		t.Fatalf("pushed move ran past a pending block: %v", log)
	}
	if len(state.Queue) != 1 || state.Queue[0].Move != "draw" {
		t.Fatalf("queue = %+v, want the OnBegin push waiting", state.Queue)
	}

	// Player 0, whose turn has ended, answers out of turn; the cascade resumes.
	state, err = Apply(game, state, MoveRequest{PlayerID: "0", Move: "pick", ResumeTag: "end-confirm"})
	if err != nil {
		t.Fatalf("answer OnEnd block: %v", err)
	}
	if len(state.Blocks) != 0 || len(state.Queue) != 0 {
		t.Fatalf("blocks=%v queue=%v, want both drained", state.Blocks, state.Queue)
	}
	if indexOf(hookQLog(state), "draw:1@1") < 0 {
		t.Fatalf("pushed move did not run after the answer: %v", hookQLog(state))
	}
}

// TestNewMatchHookQueue: the hooks NewMatch runs get a Queue. A block the
// first turn's OnBegin raises is in the initial state; a move it pushes
// has run by the time NewMatch returns.
func TestNewMatchHookQueue(t *testing.T) {
	t.Run("block", func(t *testing.T) {
		game := hookQueueGame(func(mc *MoveContext) {
			if mc.Ctx.Turn == 1 {
				startPrompt(mc, "0")
			}
		}, nil)
		state := NewMatch(game, 2, nil)
		if len(state.Blocks) != 1 || state.Blocks[0].Tag != "start-pick" || state.Blocks[0].PlayerID != "0" {
			t.Fatalf("initial blocks = %+v, want the opening prompt for 0", state.Blocks)
		}
		if _, err := Apply(game, state, MoveRequest{PlayerID: "0", Move: "noop"}); !errors.Is(err, ErrBlocked) {
			t.Fatalf("noop err = %v, want ErrBlocked", err)
		}
		state, err := Apply(game, state, MoveRequest{
			PlayerID: "0", Move: "pick", Args: []any{2}, ResumeTag: "start-pick",
		})
		if err != nil {
			t.Fatalf("answer opening prompt: %v", err)
		}
		if len(state.Blocks) != 0 {
			t.Fatalf("blocks = %v, want none", state.Blocks)
		}
	})
	t.Run("push", func(t *testing.T) {
		game := hookQueueGame(func(mc *MoveContext) {
			if mc.Ctx.Turn == 1 {
				mc.Queue.Push(mc.Ctx.CurrentPlayer, "draw")
			}
		}, nil)
		state := NewMatch(game, 2, nil)
		want := []string{"begin:0@1", "draw:0@0"}
		if got := hookQLog(state); !reflect.DeepEqual(got, want) {
			t.Fatalf("log = %v, want %v", got, want)
		}
		if len(state.Queue) != 0 {
			t.Fatalf("queue = %v, want drained", state.Queue)
		}
		found := false
		for _, e := range state.Log {
			if e.Kind == "drain-step" && e.Move == "draw" {
				found = true
				if e.Parent != -1 {
					t.Fatalf("setup drain step Parent = %d, want -1 (no move started it)", e.Parent)
				}
			}
		}
		if !found {
			t.Fatalf("no drain-step entry for draw in %+v", state.Log)
		}
		// Replay reconstructs the same opening.
		replayed, err := Replay(game, state.Log, 2, nil)
		if err != nil {
			t.Fatalf("replay: %v", err)
		}
		if !reflect.DeepEqual(hookQLog(replayed), want) {
			t.Fatalf("replayed log = %v, want %v", hookQLog(replayed), want)
		}
	})
}

// TestNewMatchCascadeFailureStaysQueued: NewMatch cannot return an error,
// so a failing setup cascade is rolled back to where it started and its
// moves stay in State.Queue. Nothing is dropped: the next Apply runs them
// again after its move and reports the failure.
func TestNewMatchCascadeFailureStaysQueued(t *testing.T) {
	game := hookQueueGame(func(mc *MoveContext) {
		if mc.Ctx.Turn == 1 {
			mc.Queue.Push(mc.Ctx.CurrentPlayer, "draw")
			mc.Queue.Push(mc.Ctx.CurrentPlayer, "fail")
		}
	}, nil)
	state := NewMatch(game, 2, nil)
	if got, want := hookQLog(state), []string{"begin:0@1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("log = %v, want %v (cascade rolled back, hook kept)", got, want)
	}
	if len(state.Queue) != 2 || state.Queue[0].Move != "draw" || state.Queue[1].Move != "fail" {
		t.Fatalf("queue = %+v, want [draw fail] still queued", state.Queue)
	}
	for _, e := range state.Log {
		if e.Kind == "drain-step" {
			t.Fatalf("rolled-back cascade left a log entry: %+v", e)
		}
	}
	if _, err := Apply(game, state, MoveRequest{PlayerID: "0", Move: "noop"}); !errors.Is(err, errHookQFail) {
		t.Fatalf("next Apply err = %v, want the queued step's error", err)
	}
}

// TestForceEndTurnHookQueue: the hooks ForceEndTurn runs get a Queue. A
// block the next turn's OnBegin raises is harvested into State.Blocks; a
// move it pushes is drained before ForceEndTurn returns.
func TestForceEndTurnHookQueue(t *testing.T) {
	t.Run("block", func(t *testing.T) {
		game := hookQueueGame(func(mc *MoveContext) {
			if mc.Ctx.Turn == 2 {
				startPrompt(mc, mc.Ctx.CurrentPlayer)
			}
		}, nil)
		state := ForceEndTurn(game, NewMatch(game, 2, nil))
		if state.Ctx.CurrentPlayer != "1" || state.Ctx.Turn != 2 {
			t.Fatalf("turn not forced: current=%s turn=%d", state.Ctx.CurrentPlayer, state.Ctx.Turn)
		}
		if len(state.Blocks) != 1 || state.Blocks[0].Tag != "start-pick" || state.Blocks[0].PlayerID != "1" {
			t.Fatalf("blocks = %+v, want the OnBegin prompt for 1", state.Blocks)
		}
		if _, err := Apply(game, state, MoveRequest{PlayerID: "1", Move: "noop"}); !errors.Is(err, ErrBlocked) {
			t.Fatalf("noop err = %v, want ErrBlocked", err)
		}
		if _, err := Apply(game, state, MoveRequest{
			PlayerID: "1", Move: "pick", Args: []any{1}, ResumeTag: "start-pick",
		}); err != nil {
			t.Fatalf("answer prompt: %v", err)
		}
	})
	t.Run("push", func(t *testing.T) {
		game := hookQueueGame(func(mc *MoveContext) {
			if mc.Ctx.Turn == 2 {
				mc.Queue.Push(mc.Ctx.CurrentPlayer, "draw")
			}
		}, nil)
		state := ForceEndTurn(game, NewMatch(game, 2, nil))
		want := []string{"begin:0@1", "turnEnd:0@1", "begin:1@2", "draw:1@1"}
		if got := hookQLog(state); !reflect.DeepEqual(got, want) {
			t.Fatalf("log = %v, want %v", got, want)
		}
		if len(state.Queue) != 0 {
			t.Fatalf("queue = %v, want drained", state.Queue)
		}
	})
}

// TestForceEndTurnCascadeFailureIsAtomic: ForceEndTurn is atomic like
// Apply. When the cascade its hooks start fails, it returns the state it
// was given, just as Apply refuses a move that ends the turn into the same
// failure.
func TestForceEndTurnCascadeFailureIsAtomic(t *testing.T) {
	game := hookQueueGame(func(mc *MoveContext) {
		if mc.Ctx.Turn == 2 {
			mc.Queue.Push(mc.Ctx.CurrentPlayer, "fail")
		}
	}, nil)
	before := NewMatch(game, 2, nil)
	after := ForceEndTurn(game, before)
	if after.Ctx.CurrentPlayer != "0" || after.Ctx.Turn != 1 {
		t.Fatalf("failed cascade still ended the turn: current=%s turn=%d", after.Ctx.CurrentPlayer, after.Ctx.Turn)
	}
	if !reflect.DeepEqual(hookQLog(after), hookQLog(before)) || len(after.Queue) != 0 || len(after.Blocks) != 0 {
		t.Fatalf("state changed: log=%v queue=%v blocks=%v", hookQLog(after), after.Queue, after.Blocks)
	}
	if len(after.Log) != len(before.Log) {
		t.Fatalf("log grew from %d to %d entries", len(before.Log), len(after.Log))
	}
	// The move path refuses the same transition.
	if _, err := Apply(game, before, MoveRequest{PlayerID: "0", Move: "end"}); !errors.Is(err, errHookQFail) {
		t.Fatalf("Apply end err = %v, want the step's error", err)
	}
}

// TestDropBlocksForKeepsHookRaisedBlock: a timeout-style move drops the
// stranded blocks of player 0 and ends the turn, and the next turn's
// OnBegin raises a fresh block for player 0. DropBlocksFor applies before
// new blocks merge, so the fresh block survives and the stale one is gone.
func TestDropBlocksForKeepsHookRaisedBlock(t *testing.T) {
	game := hookQueueGame(func(mc *MoveContext) {
		if mc.Ctx.Turn == 2 {
			mc.Queue.Block("fresh", "0", nil)
		}
	}, nil)
	state := NewMatch(game, 2, nil)
	state, err := Apply(game, state, MoveRequest{PlayerID: "0", Move: "ask"})
	if err != nil {
		t.Fatalf("ask: %v", err)
	}
	if len(state.Blocks) != 1 || state.Blocks[0].Tag != "stale" {
		t.Fatalf("blocks = %+v, want the stale prompt", state.Blocks)
	}
	state, err = Apply(game, state, MoveRequest{PlayerID: "1", Move: "forceTimeout"})
	if err != nil {
		t.Fatalf("forceTimeout: %v", err)
	}
	if state.Ctx.CurrentPlayer != "1" {
		t.Fatalf("turn did not rotate: current=%s", state.Ctx.CurrentPlayer)
	}
	if len(state.Blocks) != 1 || state.Blocks[0].Tag != "fresh" || state.Blocks[0].PlayerID != "0" {
		t.Fatalf("blocks = %+v, want only the fresh OnBegin block for 0", state.Blocks)
	}
}

// TestRunMoveChainedMoveCanBlock: a move run through Events.RunMove shares
// its parent's Queue, so a block it raises is harvested like the parent's.
func TestRunMoveChainedMoveCanBlock(t *testing.T) {
	game := hookQueueGame(nil, nil)
	state := NewMatch(game, 2, nil)
	state, err := Apply(game, state, MoveRequest{PlayerID: "0", Move: "chain"})
	if err != nil {
		t.Fatalf("chain: %v", err)
	}
	if indexOf(hookQLog(state), "chained:0") < 0 {
		t.Fatalf("chained move did not run: %v", hookQLog(state))
	}
	if len(state.Blocks) != 1 || state.Blocks[0].Tag != "chained" || state.Blocks[0].PlayerID != "0" {
		t.Fatalf("blocks = %+v, want the chained move's block", state.Blocks)
	}
}

// TestHookApplyCascadeFailureRollsBackMove: Apply keeps its all-or-nothing
// behavior for moves a hook pushed. A failing step refuses the move that
// ended the turn and leaves the caller's state as it was.
func TestHookApplyCascadeFailureRollsBackMove(t *testing.T) {
	game := hookQueueGame(func(mc *MoveContext) {
		if mc.Ctx.Turn == 2 {
			mc.Queue.Push(mc.Ctx.CurrentPlayer, "draw")
			mc.Queue.Push(mc.Ctx.CurrentPlayer, "fail")
		}
	}, nil)
	before := NewMatch(game, 2, nil)
	after, err := Apply(game, before, MoveRequest{PlayerID: "0", Move: "end"})
	if !errors.Is(err, errHookQFail) {
		t.Fatalf("err = %v, want the step's error", err)
	}
	if after.Ctx.Turn != 1 || after.StateID != before.StateID || len(after.Queue) != 0 {
		t.Fatalf("refused move changed state: turn=%d stateID=%d queue=%v", after.Ctx.Turn, after.StateID, after.Queue)
	}
}

// TestQuestionCallbacksHaveNoQueue: TurnOrder.Next and Phase.Next answer
// "who/what is next?" and get neither Events nor a Queue, while the hooks
// around them get both.
func TestQuestionCallbacksHaveNoQueue(t *testing.T) {
	var seen []string
	record := func(name string, mc *MoveContext) {
		seen = append(seen, fmt.Sprintf("%s events=%t queue=%t", name, mc.Events != nil, mc.Queue != nil))
	}
	game := &Game{
		Name:       "question-callbacks",
		MinPlayers: 2,
		MaxPlayers: 2,
		Setup:      func(_ Ctx, _ any) G { return &hookQState{} },
		Moves: map[string]any{
			"end": MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
				mc.Events.EndTurn()
				return mc.G, nil
			}),
			"endPhase": MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
				mc.Events.EndPhase()
				return mc.G, nil
			}),
		},
		Phases: map[string]*PhaseConfig{
			"a": {
				Start: true,
				Next: func(mc *MoveContext) string {
					record("Phase.Next", mc)
					return "b"
				},
				OnEnd: func(mc *MoveContext) G {
					record("Phase.OnEnd", mc)
					return mc.G
				},
				Turn: &TurnConfig{
					Order: TurnOrder{
						First: func(mc *MoveContext) int { return 0 },
						Next: func(mc *MoveContext) *int {
							record("TurnOrder.Next", mc)
							n := (mc.Ctx.PlayOrderPos + 1) % mc.Ctx.NumPlayers
							return &n
						},
					},
					OnBegin: func(mc *MoveContext) G {
						record("Turn.OnBegin", mc)
						return mc.G
					},
				},
			},
			"b": {},
		},
	}
	state := NewMatch(game, 2, nil)
	state, err := Apply(game, state, MoveRequest{PlayerID: "0", Move: "end"})
	if err != nil {
		t.Fatalf("end: %v", err)
	}
	if _, err := Apply(game, state, MoveRequest{PlayerID: "1", Move: "endPhase"}); err != nil {
		t.Fatalf("endPhase: %v", err)
	}
	want := []string{
		"Turn.OnBegin events=true queue=true", // setup
		"TurnOrder.Next events=false queue=false",
		"Turn.OnBegin events=true queue=true",
		"Phase.Next events=false queue=false",
		"Phase.OnEnd events=true queue=true",
	}
	if !reflect.DeepEqual(seen, want) {
		t.Fatalf("callbacks saw:\n  %s\nwant:\n  %s", strings.Join(seen, "\n  "), strings.Join(want, "\n  "))
	}
}
