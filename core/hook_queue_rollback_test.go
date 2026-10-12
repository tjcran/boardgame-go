package core

import (
	"errors"
	"testing"
)

// tallyPlugin keeps an int. Its API is a pointer to a private copy and
// its Flush returns the value, so every flush writes a fresh value into
// the State.Plugins map: exactly the in-place container write a rolled-
// back cascade must not leak into the state it rolls back to.
type tallyPlugin struct{}

func (tallyPlugin) Name() string                              { return "tally" }
func (tallyPlugin) Setup(_ G, _ Ctx, _ *Game) any             { return 0 }
func (tallyPlugin) Flush(_, api any, _ G, _ Ctx, _ *Game) any { return *api.(*int) }
func (tallyPlugin) API(data any, _ G, _ Ctx, _ string, _ *Game) any {
	n, _ := data.(int)
	return &n
}

// bumpTally increments the tally plugin through mc's API object. A no-op
// for games without the plugin.
func bumpTally(mc *MoveContext) {
	if n, ok := mc.Plugin("tally").(*int); ok {
		*n++
	}
}

func tallyOf(s State) any { return s.Plugins["tally"] }

func drainSteps(s State) int {
	n := 0
	for _, e := range s.Log {
		if e.Kind == "drain-step" {
			n++
		}
	}
	return n
}

// TestNewMatchRollbackKeepsContainers: a failed setup cascade is rolled
// back to where it started, including the containers its steps wrote in
// place. The first turn's OnBegin bumps the tally and pushes a step that
// moves player 0 to another stage, a step that bumps the tally again,
// and a failing step. The returned state keeps the hook's bump and the
// starting stage, and reports the failure through NewMatchE.
func TestNewMatchRollbackKeepsContainers(t *testing.T) {
	game := hookQueueGame(func(mc *MoveContext) {
		if mc.Ctx.Turn == 1 {
			bumpTally(mc)
			mc.Queue.Push("0", "toStage")
			mc.Queue.Push("0", "bump")
			mc.Queue.Push("0", "fail")
		}
	}, nil)
	game.Turn.ActivePlayers = &ActivePlayersConfig{CurrentPlayer: Stage("main")}
	game.Plugins = []Plugin{tallyPlugin{}}

	state, err := NewMatchE(game, 2, nil)
	if !errors.Is(err, errHookQFail) {
		t.Fatalf("NewMatchE err = %v, want the step's error", err)
	}
	if got := state.Ctx.ActivePlayers["0"]; got != "main" {
		t.Fatalf("player 0 stage = %q, want main (toStage rolled back)", got)
	}
	if got := tallyOf(state); got != 1 {
		t.Fatalf("tally = %v, want 1 (hook kept, step rolled back)", got)
	}
	if len(state.Queue) != 3 || drainSteps(state) != 0 {
		t.Fatalf("queue=%+v drain steps=%d, want all three queued and none logged", state.Queue, drainSteps(state))
	}

	plain := NewMatch(game, 2, nil)
	if plain.Ctx.ActivePlayers["0"] != "main" || tallyOf(plain) != 1 || len(plain.Queue) != 3 {
		t.Fatalf("NewMatch disagrees with NewMatchE: stage=%q tally=%v queue=%+v",
			plain.Ctx.ActivePlayers["0"], tallyOf(plain), plain.Queue)
	}
}

// TestApplyRollbackKeepsCallerContainers: Apply's cascade stays atomic
// when a hook starts it. Player 0 enters stage "s", whose OnBegin pushes a
// stage change, a tally bump and a failing step. Apply fails, and the
// caller's state keeps its stage map and plugin data.
func TestApplyRollbackKeepsCallerContainers(t *testing.T) {
	game := hookQueueGame(nil, nil)
	game.Turn.ActivePlayers = &ActivePlayersConfig{CurrentPlayer: Stage("main")}
	game.Turn.Stages = map[string]*StageConfig{"s": {OnBegin: func(mc *MoveContext) G {
		mc.Queue.Push(mc.PlayerID, "toStage")
		mc.Queue.Push(mc.PlayerID, "bump")
		mc.Queue.Push(mc.PlayerID, "fail")
		return mc.G
	}}}
	game.Plugins = []Plugin{tallyPlugin{}}
	game.Moves["enterS"] = MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
		bumpTally(mc)
		mc.Events.SetStage("s")
		return mc.G, nil
	})

	before := NewMatch(game, 2, nil)
	after, err := Apply(game, before, MoveRequest{PlayerID: "0", Move: "enterS"})
	if !errors.Is(err, errHookQFail) {
		t.Fatalf("Apply err = %v, want the step's error", err)
	}
	for name, s := range map[string]State{"caller's": before, "returned": after} {
		if got := s.Ctx.ActivePlayers["0"]; got != "main" {
			t.Fatalf("%s stage = %q, want main", name, got)
		}
		if got := tallyOf(s); got != 0 {
			t.Fatalf("%s tally = %v, want 0", name, got)
		}
		if s.StateID != 0 || len(s.Log) != 0 || len(s.Queue) != 0 {
			t.Fatalf("%s state changed: stateID=%d log=%d queue=%d", name, s.StateID, len(s.Log), len(s.Queue))
		}
	}
}

// TestForceEndTurnRollbackKeepsCallerContainers: ForceEndTurn writes into
// private containers, so a failed cascade hands back the caller's state
// untouched. Turn.OnEnd bumps the tally (flushed into State.Plugins
// before the cascade) and the next OnBegin pushes a failing step.
func TestForceEndTurnRollbackKeepsCallerContainers(t *testing.T) {
	game := hookQueueGame(
		func(mc *MoveContext) {
			if mc.Ctx.Turn == 2 {
				mc.Queue.Push(mc.Ctx.CurrentPlayer, "fail")
			}
		},
		func(mc *MoveContext) { bumpTally(mc) },
	)
	game.Plugins = []Plugin{tallyPlugin{}}

	before := NewMatch(game, 2, nil)
	after, err := ForceEndTurnE(game, before)
	if !errors.Is(err, errHookQFail) {
		t.Fatalf("ForceEndTurnE err = %v, want the step's error", err)
	}
	for name, s := range map[string]State{"caller's": before, "returned": after} {
		if got := tallyOf(s); got != 0 {
			t.Fatalf("%s tally = %v, want 0 (OnEnd's flush leaked)", name, got)
		}
		if s.Ctx.Turn != 1 || s.Ctx.CurrentPlayer != "0" || s.StateID != 0 {
			t.Fatalf("%s state changed: turn=%d current=%s stateID=%d", name, s.Ctx.Turn, s.Ctx.CurrentPlayer, s.StateID)
		}
	}
}

// TestDrainOverflowOutsideMoves: a hook that starts an endless cascade
// overflows MaxDrainDepth at setup and on a forced turn end. NewMatchE
// reports it and rolls the cascade back; ForceEndTurnE reports it and
// hands back its input.
func TestDrainOverflowOutsideMoves(t *testing.T) {
	t.Run("NewMatch", func(t *testing.T) {
		game := hookQueueGame(func(mc *MoveContext) {
			if mc.Ctx.Turn == 1 {
				mc.Queue.Push("0", "loop")
			}
		}, nil)
		state, err := NewMatchE(game, 2, nil)
		if !errors.Is(err, ErrDrainOverflow) {
			t.Fatalf("NewMatchE err = %v, want ErrDrainOverflow", err)
		}
		if len(state.Queue) != 1 || state.Queue[0].Move != "loop" || drainSteps(state) != 0 {
			t.Fatalf("queue=%+v drain steps=%d, want the hook's push queued and nothing logged",
				state.Queue, drainSteps(state))
		}
	})
	t.Run("ForceEndTurn", func(t *testing.T) {
		game := hookQueueGame(func(mc *MoveContext) {
			if mc.Ctx.Turn == 2 {
				mc.Queue.Push(mc.Ctx.CurrentPlayer, "loop")
			}
		}, nil)
		before := NewMatch(game, 2, nil)
		after, err := ForceEndTurnE(game, before)
		if !errors.Is(err, ErrDrainOverflow) {
			t.Fatalf("ForceEndTurnE err = %v, want ErrDrainOverflow", err)
		}
		if after.Ctx.Turn != 1 || len(after.Queue) != 0 || len(after.Log) != len(before.Log) {
			t.Fatalf("state changed: turn=%d queue=%+v log=%d", after.Ctx.Turn, after.Queue, len(after.Log))
		}
	})
}

// TestForceEndTurnBumpsStateIDOnce: a forced turn end is a state change,
// so a client holding the previous StateID is told it is stale. Drain
// steps it runs do not bump further, as in Apply.
func TestForceEndTurnBumpsStateIDOnce(t *testing.T) {
	game := hookQueueGame(func(mc *MoveContext) {
		if mc.Ctx.Turn == 2 {
			mc.Queue.Push(mc.Ctx.CurrentPlayer, "draw")
		}
	}, nil)
	state := mustApply(t, game, NewMatch(game, 2, nil), "0", "noop")
	if state.StateID != 1 {
		t.Fatalf("stateID after noop = %d, want 1", state.StateID)
	}
	forced, err := ForceEndTurnE(game, state)
	if err != nil {
		t.Fatalf("ForceEndTurnE: %v", err)
	}
	if forced.StateID != 2 {
		t.Fatalf("stateID after forced turn end = %d, want 2", forced.StateID)
	}
	if !hasLogPrefix(hookQLog(forced), "draw:1@1") {
		t.Fatalf("log = %v, want OnBegin's draw run", hookQLog(forced))
	}
	if _, err := Apply(game, forced, MoveRequest{PlayerID: "1", Move: "noop", StateID: 1}); !errors.Is(err, ErrStaleState) {
		t.Fatalf("move with pre-expiry StateID err = %v, want ErrStaleState", err)
	}
	if _, err := Apply(game, forced, MoveRequest{PlayerID: "1", Move: "noop", StateID: 2}); err != nil {
		t.Fatalf("move with current StateID: %v", err)
	}
}

// TestForceEndTurnEGameOver: forcing a turn end on a finished game is an
// error for ForceEndTurnE and a no-op for ForceEndTurn.
func TestForceEndTurnEGameOver(t *testing.T) {
	game := hookQueueGame(nil, nil)
	game.Moves["finish"] = MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
		mc.Events.EndGame("done")
		return mc.G, nil
	})
	over := mustApply(t, game, NewMatch(game, 2, nil), "0", "finish")
	if _, err := ForceEndTurnE(game, over); !errors.Is(err, ErrGameOver) {
		t.Fatalf("ForceEndTurnE err = %v, want ErrGameOver", err)
	}
	if got := ForceEndTurn(game, over); got.StateID != over.StateID || got.Ctx.Turn != over.Ctx.Turn {
		t.Fatalf("ForceEndTurn changed a finished game: stateID %d->%d turn %d->%d",
			over.StateID, got.StateID, over.Ctx.Turn, got.Ctx.Turn)
	}
}
