package ccg_test

import (
	"fmt"

	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/modules/ccg"
)

// ExampleCombat walks one declaration round: attacks are declared,
// the legal blocks are enumerated and offered as an assignment
// request, the defender's answer is validated, and the unblocked
// attackers are read back.
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
	req := core.AssignmentRequest{Kind: "blocks", AllowEmpty: true}
	for _, p := range offered {
		req.Pairs = append(req.Pairs, [2]any{p[0], p[1]})
	}

	// The player's answer: 10 blocks attacker 1.
	chosen := [][2]any{{ccg.EntityID(10), ccg.EntityID(1)}}
	if err := core.ValidateAssignment(req, chosen); err != nil {
		fmt.Println("validate:", err)
		return
	}
	var pairs [][2]ccg.EntityID
	for _, p := range chosen {
		pairs = append(pairs, [2]ccg.EntityID{p[0].(ccg.EntityID), p[1].(ccg.EntityID)})
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
