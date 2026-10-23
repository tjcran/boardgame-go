package ccg_test

import (
	"fmt"

	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/modules/ccg"
)

// ExampleCombat walks one declaration round: attacks are declared,
// the legal blocks are offered as an assignment request, the
// defender's answer is validated and converted back to entity pairs,
// and the unblocked attackers are read back.
func ExampleCombat() {
	var c ccg.Combat
	err := c.DeclareAttacks([]ccg.Attack{
		{Attacker: 1, Target: ccg.CombatTarget{Player: "1"}},
		{Attacker: 2, Target: ccg.CombatTarget{Player: "1"}},
	}, nil)
	if err != nil {
		fmt.Println("declare attacks:", err)
		return
	}

	// Blocker 10 may block anything; blocker 11 may only block attacker 2.
	legal := func(blocker, attacker ccg.EntityID) bool {
		return blocker == 10 || attacker == 2
	}
	offered := c.LegalBlocks([]ccg.EntityID{11, 10}, legal)
	fmt.Println("offered:", offered)

	// The offered pairs become the request a player answers, usually via
	// Queue.RequestAssignment from inside a move.
	req := core.AssignmentRequest{Kind: "blocks", Pairs: ccg.ToAssignmentPairs(offered), AllowEmpty: true}

	// The resume move's args, one pair per arg: 10 blocks attacker 1. Over
	// a transport they arrive as []any{float64, float64}; both shapes
	// validate the same way.
	args := []any{[]any{float64(10), float64(1)}}
	chosen, err := core.ValidateAssignment(req, args)
	if err != nil {
		fmt.Println("validate:", err)
		return
	}
	pairs, err := ccg.EntityPairs(chosen)
	if err != nil {
		fmt.Println("convert:", err)
		return
	}
	if err := c.DeclareBlocks(pairs, legal, nil); err != nil {
		fmt.Println("declare blocks:", err)
		return
	}

	fmt.Println("blockers of 1:", c.BlockersOf(1))
	for _, a := range c.Unblocked() {
		fmt.Println("unblocked attacker:", a.Attacker)
	}
	// Output:
	// offered: [[10 1] [10 2] [11 2]]
	// blockers of 1: [10]
	// unblocked attacker: 2
}
