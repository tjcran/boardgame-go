package ccg_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/modules/ccg"
)

// combatG is a minimal game state: bookkeeping plus a Combat round.
type combatG struct {
	*ccg.State
	Combat ccg.Combat
}

var errNoAssignment = errors.New("assign: not resuming an assignment block")

// assignmentCombatGame wires the three primitives through the real
// reducer: "declare" (player 0) records attacks and asks player 1 for
// blocks via an AssignmentRequest answered by "assign"; player 1 is
// confined to an Exclusive stage whose table does not list "assign",
// so the answer only works because the named block exempts it.
func assignmentCombatGame(t *testing.T) *core.Game {
	t.Helper()
	const attackerA, attackerB, blockerX, blockerY = ccg.EntityID(1), ccg.EntityID(2), ccg.EntityID(3), ccg.EntityID(4)
	legal := func(b, a ccg.EntityID) bool { return true }
	stage := "respond"
	return &core.Game{
		Name:       "assignment-combat-test",
		MinPlayers: 2,
		MaxPlayers: 2,
		Setup:      func(_ core.Ctx, _ any) core.G { return &combatG{State: ccg.NewState()} },
		Moves: map[string]any{
			"declare": core.MoveFn(func(mc *core.MoveContext, _ ...any) (core.G, error) {
				g := mc.G.(*combatG)
				attacks := []ccg.Attack{
					{Attacker: attackerA, Target: ccg.CombatTarget{Player: "1"}},
					{Attacker: attackerB, Target: ccg.CombatTarget{Player: "1"}},
				}
				if err := g.Combat.DeclareAttacks(attacks, nil); err != nil {
					return nil, err
				}
				pairs := g.Combat.LegalBlocks([]ccg.EntityID{blockerX, blockerY}, legal)
				req := core.AssignmentRequest{Kind: "blocks", Pairs: ccg.ToAssignmentPairs(pairs), AllowEmpty: true}
				mc.Events.SetActivePlayers(core.ActivePlayersConfig{Others: &stage})
				mc.Queue.RequestAssignment("1", req, core.AnsweredBy("assign"))
				return g, nil
			}),
			"assign": core.MoveFn(func(mc *core.MoveContext, args ...any) (core.G, error) {
				g := mc.G.(*combatG)
				if mc.ResumingBlock == nil || mc.ResumingBlock.Assignment == nil {
					return nil, errNoAssignment
				}
				chosen, err := core.ValidateAssignment(*mc.ResumingBlock.Assignment, args)
				if err != nil {
					return nil, err
				}
				pairs, err := ccg.EntityPairs(chosen)
				if err != nil {
					return nil, err
				}
				if err := g.Combat.DeclareBlocks(pairs, legal, nil); err != nil {
					return nil, err
				}
				return g, nil
			}),
			"other": core.MoveFn(func(mc *core.MoveContext, _ ...any) (core.G, error) { return mc.G, nil }),
		},
		Turn: &core.TurnConfig{
			Stages: map[string]*core.StageConfig{
				// Exclusive with a table that omits "assign".
				"respond": {Exclusive: true, Moves: map[string]any{
					"respondOnly": core.MoveFn(func(mc *core.MoveContext, _ ...any) (core.G, error) { return mc.G, nil }),
				}},
			},
		},
	}
}

// overWire returns req as a server receives it: the request is
// marshalled and decoded back, so every pair arg arrives as
// []any{float64, float64}.
func overWire(t *testing.T, req core.MoveRequest) core.MoveRequest {
	t.Helper()
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	var out core.MoveRequest
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestCombat_AssignmentAnsweredOutOfTurnInsideExclusiveStage(t *testing.T) {
	game := assignmentCombatGame(t)
	start := func(t *testing.T) core.State {
		t.Helper()
		s := core.NewMatch(game, 2, nil)
		s, err := core.Apply(game, s, core.MoveRequest{PlayerID: "0", Move: "declare"})
		if err != nil {
			t.Fatalf("declare: %v", err)
		}
		if len(s.Blocks) != 1 || s.Blocks[0].Assignment == nil {
			t.Fatalf("want one assignment block, got %+v", s.Blocks)
		}
		return s
	}
	// answer builds a resume request with one pair per arg, then sends
	// it through JSON the way a transport would.
	answer := func(t *testing.T, player string, pairs ...[2]ccg.EntityID) core.MoveRequest {
		t.Helper()
		var args []any
		for _, p := range ccg.ToAssignmentPairs(pairs) {
			args = append(args, p)
		}
		return overWire(t, core.MoveRequest{PlayerID: player, Move: "assign", Args: args, ResumeTag: "blocks"})
	}
	// refused applies req, requires err to match want, and requires the
	// state to be exactly as it was: block pending, round undeclared.
	refused := func(t *testing.T, s core.State, req core.MoveRequest, want error) {
		t.Helper()
		before := mustJSON(t, s)
		got, err := core.Apply(game, s, req)
		if !errors.Is(err, want) {
			t.Fatalf("%s by %s: err = %v, want %v", req.Move, req.PlayerID, err, want)
		}
		if after := mustJSON(t, s); after != before {
			t.Fatalf("refused move mutated state:\n before %s\n after  %s", before, after)
		}
		if returned := mustJSON(t, got); returned != before {
			t.Fatalf("refused move returned a different state:\n before   %s\n returned %s", before, returned)
		}
		if len(s.Blocks) != 1 || s.G.(*combatG).Combat.BlocksDeclared {
			t.Fatalf("block must stay pending and combat undeclared: blocks=%+v combat=%+v", s.Blocks, s.G.(*combatG).Combat)
		}
	}

	t.Run("addressee answers out of turn inside an exclusive stage", func(t *testing.T) {
		s := start(t)
		// The request holds typed IDs; the decoded answer holds float64s.
		if _, ok := s.Blocks[0].Assignment.Pairs[0][0].(ccg.EntityID); !ok {
			t.Fatalf("request pair side is %T, want ccg.EntityID", s.Blocks[0].Assignment.Pairs[0][0])
		}
		req := answer(t, "1", [2]ccg.EntityID{3, 1})
		if _, ok := req.Args[0].([]any); !ok {
			t.Fatalf("decoded arg is %T, want []any", req.Args[0])
		}
		next, err := core.Apply(game, s, req)
		if err != nil {
			t.Fatalf("assign: %v", err)
		}
		c := &next.G.(*combatG).Combat
		if !c.BlocksDeclared || !c.Blocked(1) || c.Blocked(2) {
			t.Fatalf("combat = %+v", c)
		}
		if got := c.BlockersOf(1); len(got) != 1 || got[0] != 3 {
			t.Fatalf("blockers of 1 = %v", got)
		}
		if got := c.Unblocked(); len(got) != 1 || got[0].Attacker != 2 {
			t.Fatalf("unblocked = %+v", got)
		}
		if len(next.Blocks) != 0 {
			t.Fatalf("block not consumed: %+v", next.Blocks)
		}
	})

	t.Run("an empty answer declares no blocks", func(t *testing.T) {
		s := start(t)
		next, err := core.Apply(game, s, answer(t, "1"))
		if err != nil {
			t.Fatalf("assign: %v", err)
		}
		c := &next.G.(*combatG).Combat
		if !c.BlocksDeclared || len(c.Unblocked()) != 2 {
			t.Fatalf("combat = %+v", c)
		}
	})

	t.Run("a player other than the addressee cannot answer", func(t *testing.T) {
		s := start(t)
		refused(t, s, answer(t, "0", [2]ccg.EntityID{3, 1}), core.ErrUnknownResumeTag)
	})

	t.Run("a pair that was not offered is rejected", func(t *testing.T) {
		s := start(t)
		refused(t, s, answer(t, "1", [2]ccg.EntityID{3, 99}), core.ErrAssignmentNotLegal)
	})

	t.Run("the whole list in one arg is malformed, not an empty answer", func(t *testing.T) {
		s := start(t)
		req := overWire(t, core.MoveRequest{PlayerID: "1", Move: "assign", ResumeTag: "blocks",
			Args: []any{ccg.ToAssignmentPairs([][2]ccg.EntityID{{3, 1}})}})
		refused(t, s, req, core.ErrAssignmentMalformed)
	})

	t.Run("any other move from the addressee is blocked", func(t *testing.T) {
		s := start(t)
		for _, move := range []string{"other", "respondOnly"} {
			refused(t, s, core.MoveRequest{PlayerID: "1", Move: move}, core.ErrBlocked)
		}
	})
}

// TestCombat_AssignmentReplayFromJSONLog: a log that went through JSON
// (persisted, then loaded) replays to the live state, whether the live
// answer arrived in-process as typed pairs or over the wire as float64s.
func TestCombat_AssignmentReplayFromJSONLog(t *testing.T) {
	game := assignmentCombatGame(t)
	for name, wire := range map[string]bool{"in-process answer": false, "wire answer": true} {
		t.Run(name, func(t *testing.T) {
			live := core.NewMatch(game, 2, nil)
			live, err := core.Apply(game, live, core.MoveRequest{PlayerID: "0", Move: "declare"})
			if err != nil {
				t.Fatalf("declare: %v", err)
			}
			req := core.MoveRequest{PlayerID: "1", Move: "assign", ResumeTag: "blocks", Args: []any{
				[2]any{ccg.EntityID(3), ccg.EntityID(1)},
				[2]any{ccg.EntityID(4), ccg.EntityID(2)},
			}}
			if wire {
				req = overWire(t, req)
			}
			live, err = core.Apply(game, live, req)
			if err != nil {
				t.Fatalf("assign: %v", err)
			}
			if c := live.G.(*combatG).Combat; !c.Blocked(1) || !c.Blocked(2) {
				t.Fatalf("live combat = %+v", c)
			}

			var log []core.LogEntry
			if err := json.Unmarshal([]byte(mustJSON(t, live.Log)), &log); err != nil {
				t.Fatal(err)
			}
			replayed, err := core.Replay(game, log, 2, nil)
			if err != nil {
				t.Fatalf("replay: %v", err)
			}
			if a, b := mustJSON(t, live.G), mustJSON(t, replayed.G); a != b {
				t.Fatalf("replay diverged:\n live %s\n repl %s", a, b)
			}
			if replayed.StateID != live.StateID {
				t.Fatalf("StateID: replay %d, live %d", replayed.StateID, live.StateID)
			}
			if a, b := mustJSON(t, live.Ctx), mustJSON(t, replayed.Ctx); a != b {
				t.Fatalf("ctx diverged:\n live %s\n repl %s", a, b)
			}
			if len(replayed.Blocks) != 0 {
				t.Fatalf("replayed blocks = %+v", replayed.Blocks)
			}
		})
	}
}
