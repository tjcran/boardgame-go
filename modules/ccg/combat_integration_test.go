package ccg_test

import (
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
				req := core.AssignmentRequest{Kind: "blocks", AllowEmpty: true}
				for _, p := range pairs {
					req.Pairs = append(req.Pairs, [2]any{p[0], p[1]})
				}
				mc.Events.SetActivePlayers(core.ActivePlayersConfig{Others: &stage})
				mc.Queue.RequestAssignment("1", req, core.AnsweredBy("assign"))
				return g, nil
			}),
			"assign": core.MoveFn(func(mc *core.MoveContext, args ...any) (core.G, error) {
				g := mc.G.(*combatG)
				chosen, _ := args[0].([][2]any)
				if err := core.ValidateAssignment(*mc.ResumingBlock.Assignment, chosen); err != nil {
					return nil, err
				}
				pairs := make([][2]ccg.EntityID, len(chosen))
				for i, c := range chosen {
					pairs[i] = [2]ccg.EntityID{c[0].(ccg.EntityID), c[1].(ccg.EntityID)}
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
	answer := func(player string, pairs ...[2]any) core.MoveRequest {
		return core.MoveRequest{PlayerID: player, Move: "assign", Args: []any{pairs}, ResumeTag: "blocks"}
	}

	t.Run("addressee answers out of turn inside an exclusive stage", func(t *testing.T) {
		s := start(t)
		next, err := core.Apply(game, s, answer("1", [2]any{ccg.EntityID(3), ccg.EntityID(1)}))
		if err != nil {
			t.Fatalf("assign: %v", err)
		}
		c := &next.G.(*combatG).Combat
		if !c.BlocksDeclared || !c.Blocked(1) || c.Blocked(2) {
			t.Fatalf("combat = %+v", c)
		}
		if got := c.Unblocked(); len(got) != 1 || got[0].Attacker != 2 {
			t.Fatalf("unblocked = %+v", got)
		}
		if len(next.Blocks) != 0 {
			t.Fatalf("block not consumed: %+v", next.Blocks)
		}
	})

	t.Run("a player other than the addressee cannot answer", func(t *testing.T) {
		s := start(t)
		if _, err := core.Apply(game, s, answer("0", [2]any{ccg.EntityID(3), ccg.EntityID(1)})); err == nil {
			t.Fatal("player 0 answered player 1's assignment")
		}
		if s.G.(*combatG).Combat.BlocksDeclared {
			t.Fatal("rejected answer mutated combat")
		}
	})

	t.Run("a pair that was not offered is rejected and leaves combat untouched", func(t *testing.T) {
		s := start(t)
		_, err := core.Apply(game, s, answer("1", [2]any{ccg.EntityID(3), ccg.EntityID(99)}))
		if !errors.Is(err, core.ErrAssignmentNotLegal) {
			t.Fatalf("err = %v, want ErrAssignmentNotLegal", err)
		}
		if s.G.(*combatG).Combat.BlocksDeclared {
			t.Fatal("rejected answer mutated combat")
		}
		if len(s.Blocks) != 1 {
			t.Fatalf("block must stay pending: %+v", s.Blocks)
		}
	})

	t.Run("any other move from the addressee is blocked", func(t *testing.T) {
		s := start(t)
		for _, move := range []string{"other", "respondOnly"} {
			_, err := core.Apply(game, s, core.MoveRequest{PlayerID: "1", Move: move})
			if !errors.Is(err, core.ErrBlocked) {
				t.Errorf("%s: err = %v, want ErrBlocked", move, err)
			}
		}
	})
}
