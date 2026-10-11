package core

import (
	"errors"
	"strings"
	"testing"
)

// siteGame is hookQueueGame without its turn hooks, plus the moves that
// drive phase, stage and game-over transitions. Each hook-site case
// installs only the hook it exercises.
func siteGame() *Game {
	g := hookQueueGame(nil, nil)
	g.Turn = &TurnConfig{}
	g.Moves["setStage"] = MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
		mc.Events.SetStage("s")
		return mc.G, nil
	})
	g.Moves["leaveStage"] = MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
		mc.Events.EndStage()
		return mc.G, nil
	})
	g.Moves["endPhase"] = MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
		mc.Events.EndPhase()
		return mc.G, nil
	})
	g.Moves["finish"] = MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
		mc.Events.EndGame("done")
		return mc.G, nil
	})
	return g
}

func pushDraw(mc *MoveContext, playerID string) G {
	mc.Queue.Push(playerID, "draw")
	return mc.G
}

func mustApply(t *testing.T, g *Game, s State, playerID, move string) State {
	t.Helper()
	next, err := Apply(g, s, MoveRequest{PlayerID: playerID, Move: move})
	if err != nil {
		t.Fatalf("%s by %s: %v", move, playerID, err)
	}
	return next
}

func hasLogPrefix(log []string, prefix string) bool {
	for _, e := range log {
		if strings.HasPrefix(e, prefix) {
			return true
		}
	}
	return false
}

func wantBlock(t *testing.T, s State, tag, playerID string) {
	t.Helper()
	for _, b := range s.Blocks {
		if b.Tag == tag && b.PlayerID == playerID {
			return
		}
	}
	t.Fatalf("blocks = %+v, want %q for %q", s.Blocks, tag, playerID)
}

// TestHookSitesPushAndBlock: every kind of lifecycle hook that gets the
// Queue can actually use it. A move it pushes runs (or, after game over,
// stays queued) and a block it raises is harvested into State.Blocks.
func TestHookSitesPushAndBlock(t *testing.T) {
	cases := []struct {
		name    string
		install func(g *Game)
		run     func(t *testing.T, g *Game) State
		check   func(t *testing.T, s State)
	}{
		{
			name: "Phase.OnBegin of the start phase pushes at setup",
			install: func(g *Game) {
				g.Phases = map[string]*PhaseConfig{"a": {
					Start:   true,
					OnBegin: func(mc *MoveContext) G { return pushDraw(mc, mc.Ctx.CurrentPlayer) },
				}}
			},
			run: func(t *testing.T, g *Game) State { return NewMatch(g, 2, nil) },
			check: func(t *testing.T, s State) {
				if !hasLogPrefix(hookQLog(s), "draw:0@0") || len(s.Queue) != 0 {
					t.Fatalf("log=%v queue=%v, want draw run at setup", hookQLog(s), s.Queue)
				}
			},
		},
		{
			name: "Phase.OnBegin of an entered phase raises a block",
			install: func(g *Game) {
				g.Phases = map[string]*PhaseConfig{
					"a": {Start: true, Next: "b"},
					"b": {OnBegin: func(mc *MoveContext) G {
						mc.Queue.Block("phase-b", mc.Ctx.CurrentPlayer, nil)
						return mc.G
					}},
				}
			},
			run: func(t *testing.T, g *Game) State {
				return mustApply(t, g, NewMatch(g, 2, nil), "0", "endPhase")
			},
			check: func(t *testing.T, s State) {
				if s.Ctx.Phase != "b" {
					t.Fatalf("phase = %q, want b", s.Ctx.Phase)
				}
				wantBlock(t, s, "phase-b", s.Ctx.CurrentPlayer)
			},
		},
		{
			name: "Phase.OnEnd pushes",
			install: func(g *Game) {
				g.Phases = map[string]*PhaseConfig{
					"a": {Start: true, Next: "b",
						OnEnd: func(mc *MoveContext) G { return pushDraw(mc, "1") }},
					"b": {},
				}
			},
			run: func(t *testing.T, g *Game) State {
				return mustApply(t, g, NewMatch(g, 2, nil), "0", "endPhase")
			},
			check: func(t *testing.T, s State) {
				if !hasLogPrefix(hookQLog(s), "draw:1@") {
					t.Fatalf("log = %v, want draw by 1", hookQLog(s))
				}
			},
		},
		{
			name: "stage OnBegin raises a block for the staged player",
			install: func(g *Game) {
				g.Turn.Stages = map[string]*StageConfig{"s": {OnBegin: func(mc *MoveContext) G {
					mc.Queue.Block("stage", mc.PlayerID, nil)
					return mc.G
				}}}
			},
			run: func(t *testing.T, g *Game) State {
				return mustApply(t, g, NewMatch(g, 2, nil), "0", "setStage")
			},
			check: func(t *testing.T, s State) { wantBlock(t, s, "stage", "0") },
		},
		{
			name: "stage OnEnd pushes",
			install: func(g *Game) {
				g.Turn.Stages = map[string]*StageConfig{"s": {OnEnd: func(mc *MoveContext) G {
					return pushDraw(mc, mc.PlayerID)
				}}}
			},
			run: func(t *testing.T, g *Game) State {
				s := mustApply(t, g, NewMatch(g, 2, nil), "0", "setStage")
				return mustApply(t, g, s, "0", "leaveStage")
			},
			check: func(t *testing.T, s State) {
				if !hasLogPrefix(hookQLog(s), "draw:0@0") {
					t.Fatalf("log = %v, want draw by 0", hookQLog(s))
				}
			},
		},
		{
			name: "Turn.EndIf raises a block as it ends the turn",
			install: func(g *Game) {
				g.Turn.EndIf = func(mc *MoveContext) (bool, string) {
					if mc.Ctx.NumMoves < 1 {
						return false, ""
					}
					mc.Queue.Block("endif", mc.Ctx.CurrentPlayer, nil)
					return true, ""
				}
			},
			run: func(t *testing.T, g *Game) State {
				return mustApply(t, g, NewMatch(g, 2, nil), "0", "noop")
			},
			check: func(t *testing.T, s State) {
				if s.Ctx.CurrentPlayer != "1" {
					t.Fatalf("turn did not end: current=%s", s.Ctx.CurrentPlayer)
				}
				wantBlock(t, s, "endif", "0")
			},
		},
		{
			name: "Game.EndIf pushes",
			install: func(g *Game) {
				g.EndIf = func(mc *MoveContext) any {
					if mc.Ctx.NumMoves == 1 {
						mc.Queue.Push(mc.PlayerID, "draw")
					}
					return nil
				}
			},
			run: func(t *testing.T, g *Game) State {
				return mustApply(t, g, NewMatch(g, 2, nil), "0", "noop")
			},
			check: func(t *testing.T, s State) {
				if !hasLogPrefix(hookQLog(s), "draw:0@0") {
					t.Fatalf("log = %v, want draw by 0", hookQLog(s))
				}
			},
		},
		{
			name: "Game.OnEnd pushes and blocks; the cascade stops at game over",
			install: func(g *Game) {
				g.OnEnd = func(mc *MoveContext) G {
					mc.Queue.Block("after", "1", nil)
					return pushDraw(mc, "0")
				}
			},
			run: func(t *testing.T, g *Game) State {
				return mustApply(t, g, NewMatch(g, 2, nil), "0", "finish")
			},
			check: func(t *testing.T, s State) {
				if s.Ctx.Gameover == nil {
					t.Fatal("game not over")
				}
				wantBlock(t, s, "after", "1")
				if len(s.Queue) != 1 || s.Queue[0].Move != "draw" || hasLogPrefix(hookQLog(s), "draw:") {
					t.Fatalf("queue=%+v log=%v, want draw queued but not run", s.Queue, hookQLog(s))
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := siteGame()
			tc.install(g)
			tc.check(t, tc.run(t, g))
		})
	}
}

// TestDrainStepHooksPushAndBlock: hooks fired by a drain step's events
// share the step's Queue. Turn 2's OnBegin pushes "end" for player 1; that
// drain step ends turn 2, and turn 3's OnBegin raises a block, which the
// step harvests. The step is logged as a child of the move that started
// the cascade.
func TestDrainStepHooksPushAndBlock(t *testing.T) {
	game := hookQueueGame(func(mc *MoveContext) {
		switch mc.Ctx.Turn {
		case 2:
			mc.Queue.Push(mc.Ctx.CurrentPlayer, "end")
		case 3:
			mc.Queue.Block("turn3", mc.Ctx.CurrentPlayer, nil)
		}
	}, nil)
	state := mustApply(t, game, NewMatch(game, 2, nil), "0", "end")
	if state.Ctx.Turn != 3 || state.Ctx.CurrentPlayer != "0" {
		t.Fatalf("turn=%d current=%s, want turn 3 for 0", state.Ctx.Turn, state.Ctx.CurrentPlayer)
	}
	if len(state.Blocks) != 1 {
		t.Fatalf("blocks = %+v, want only turn 3's", state.Blocks)
	}
	wantBlock(t, state, "turn3", "0")

	external, step := -1, -1
	for i, e := range state.Log {
		switch {
		case e.Kind == "move" && e.Move == "end":
			external = i
		case e.Kind == "drain-step" && e.Move == "end":
			step = i
		}
	}
	if external < 0 || step < 0 {
		t.Fatalf("log %+v lacks the external end move or its drain step", state.Log)
	}
	if got := state.Log[step].Parent; got != external {
		t.Fatalf("drain step Parent = %d, want %d", got, external)
	}
	if state.Log[step].PlayerID != "1" {
		t.Fatalf("drain step PlayerID = %q, want 1", state.Log[step].PlayerID)
	}
}

// TestHookDropBlocksForIsHonored: a lifecycle hook's DropBlocksFor counts
// as the move's. Player 0 is stranded on a prompt; player 1 skips the turn
// with a move that drops nothing itself, and Turn.OnEnd drops the ending
// player's blocks. The fresh block the next OnBegin raises for player 0
// survives, as it does when the move drops.
func TestHookDropBlocksForIsHonored(t *testing.T) {
	game := hookQueueGame(
		func(mc *MoveContext) {
			if mc.Ctx.Turn == 2 {
				mc.Queue.Block("fresh", "0", nil)
			}
		},
		func(mc *MoveContext) { mc.DropBlocksFor(mc.Ctx.CurrentPlayer) },
	)
	state := mustApply(t, game, NewMatch(game, 2, nil), "0", "ask")
	wantBlock(t, state, "stale", "0")
	state = mustApply(t, game, state, "1", "skipTurn")
	if len(state.Blocks) != 1 {
		t.Fatalf("blocks = %+v, want only the fresh block", state.Blocks)
	}
	wantBlock(t, state, "fresh", "0")
}

// TestChainedMoveDropBlocksForIsHonored: an Events.RunMove chained move
// shares its parent's DropBlocksFor list.
func TestChainedMoveDropBlocksForIsHonored(t *testing.T) {
	game := hookQueueGame(nil, nil)
	game.Moves["expire"] = Move{
		AnyPlayer:    true,
		IgnoreBlocks: true,
		Move: MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
			mc.Events.RunMove("dropCurrent")
			return mc.G, nil
		}),
	}
	game.Moves["dropCurrent"] = MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
		mc.DropBlocksFor(mc.Ctx.CurrentPlayer)
		return mc.G, nil
	})
	state := mustApply(t, game, NewMatch(game, 2, nil), "0", "ask")
	state = mustApply(t, game, state, "1", "expire")
	if len(state.Blocks) != 0 {
		t.Fatalf("blocks = %+v, want the stale prompt dropped", state.Blocks)
	}
}

// TestForceEndTurnDropsEndingPlayersBlocks: the timed-out player's prompts
// expire with their turn. OnBegin prompts each turn's player; player 0
// never answers and the turn is forced. Only player 1's prompt remains,
// so once player 1 answers it their turn is theirs to play.
func TestForceEndTurnDropsEndingPlayersBlocks(t *testing.T) {
	game := hookQueueGame(func(mc *MoveContext) { startPrompt(mc, mc.Ctx.CurrentPlayer) }, nil)
	state := NewMatch(game, 2, nil)
	wantBlock(t, state, "start-pick", "0")

	state, err := ForceEndTurnE(game, state)
	if err != nil {
		t.Fatalf("ForceEndTurnE: %v", err)
	}
	if len(state.Blocks) != 1 {
		t.Fatalf("blocks = %+v, want only player 1's prompt", state.Blocks)
	}
	wantBlock(t, state, "start-pick", "1")

	state, err = Apply(game, state, MoveRequest{
		PlayerID: "1", Move: "pick", Args: []any{1}, ResumeTag: "start-pick",
	})
	if err != nil {
		t.Fatalf("answer: %v", err)
	}
	if _, err := Apply(game, state, MoveRequest{PlayerID: "1", Move: "noop"}); err != nil {
		t.Fatalf("player 1's turn still gated after answering their prompt: %v", err)
	}
}

// TestForceEndTurnKeepsOtherPlayersBlocks: only the ending player's
// blocks expire; a prompt pending for anyone else still gates the match.
func TestForceEndTurnKeepsOtherPlayersBlocks(t *testing.T) {
	game := hookQueueGame(nil, nil)
	game.Moves["askOther"] = MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
		mc.Queue.Block("reaction", "1", nil)
		return mc.G, nil
	})
	state := mustApply(t, game, NewMatch(game, 2, nil), "0", "askOther")
	state, err := ForceEndTurnE(game, state)
	if err != nil {
		t.Fatalf("ForceEndTurnE: %v", err)
	}
	wantBlock(t, state, "reaction", "1")
	if _, err := Apply(game, state, MoveRequest{PlayerID: "1", Move: "noop"}); !errors.Is(err, ErrBlocked) {
		t.Fatalf("noop err = %v, want ErrBlocked", err)
	}
}
