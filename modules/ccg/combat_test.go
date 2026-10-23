package ccg

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/tjcran/boardgame-go/core"
)

func allowAll(blocker, attacker EntityID) bool { return true }

func declared(t *testing.T, c *Combat, ids ...EntityID) {
	t.Helper()
	var as []Attack
	for _, id := range ids {
		as = append(as, Attack{Attacker: id, Target: CombatTarget{Player: "1"}})
	}
	if err := c.DeclareAttacks(as, nil); err != nil {
		t.Fatal(err)
	}
}

func TestCombat_DeclareAttacks_AllOrNothing(t *testing.T) {
	var c Combat
	boom := errors.New("illegal")
	as := []Attack{{Attacker: 1}, {Attacker: 2}, {Attacker: 3}}
	err := c.DeclareAttacks(as, func(a Attack) error {
		if a.Attacker == 2 {
			return boom
		}
		return nil
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	if !reflect.DeepEqual(c, Combat{}) {
		t.Fatalf("combat mutated: %+v", c)
	}
}

func TestCombat_DeclareAttacks_DuplicateAttacker(t *testing.T) {
	var c Combat
	err := c.DeclareAttacks([]Attack{{Attacker: 1}, {Attacker: 7}, {Attacker: 7}}, nil)
	if !errors.Is(err, ErrDuplicateAttacker) {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), "attacker 7") {
		t.Fatalf("err %q lacks the duplicated attacker", err)
	}
	if !reflect.DeepEqual(c, Combat{}) {
		t.Fatalf("combat mutated: %+v", c)
	}
}

func TestCombat_DeclareAttacks_Empty(t *testing.T) {
	var c Combat
	if err := c.DeclareAttacks(nil, nil); !errors.Is(err, ErrNoAttacks) {
		t.Fatalf("err = %v", err)
	}
}

func TestCombat_DeclareAttacks_Twice(t *testing.T) {
	var c Combat
	declared(t, &c, 1)
	if err := c.DeclareAttacks([]Attack{{Attacker: 2}}, nil); !errors.Is(err, ErrAttacksAlreadyDeclared) {
		t.Fatalf("err = %v", err)
	}
	if len(c.Attacks) != 1 || !c.Attacking(1) || c.Attacking(2) {
		t.Fatalf("attacks = %+v", c.Attacks)
	}
}

// TestCombat_RemoveAllAttackers_KeepsDeclared: removing every attacker
// does not reopen attack declaration, and the round can still close
// with an empty block declaration.
func TestCombat_RemoveAllAttackers_KeepsDeclared(t *testing.T) {
	var c Combat
	declared(t, &c, 10, 11)
	c.Remove(10)
	c.Remove(11)
	if len(c.Attacks) != 0 || !c.AttacksDeclared {
		t.Fatalf("after removing all attackers: %+v", c)
	}
	if err := c.DeclareAttacks([]Attack{{Attacker: 12}}, nil); !errors.Is(err, ErrAttacksAlreadyDeclared) {
		t.Fatalf("redeclare: err = %v, want ErrAttacksAlreadyDeclared", err)
	}
	if c.Attacking(12) {
		t.Fatal("refused redeclaration recorded an attacker")
	}
	if err := c.DeclareBlocks(nil, allowAll, nil); err != nil {
		t.Fatalf("closing the emptied round: %v", err)
	}
	if !c.BlocksDeclared || len(c.Unblocked()) != 0 {
		t.Fatalf("round not closed: %+v", c)
	}
	if err := c.DeclareBlocks([][2]EntityID{{1, 10}}, allowAll, nil); !errors.Is(err, ErrBlocksAlreadyDeclared) {
		t.Fatalf("err = %v", err)
	}
}

func TestCombat_RemoveAllAttackers_BlockNamingThemFails(t *testing.T) {
	var c Combat
	declared(t, &c, 10)
	c.Remove(10)
	before := c
	if err := c.DeclareBlocks([][2]EntityID{{1, 10}}, allowAll, nil); !errors.Is(err, ErrNotAttacking) {
		t.Fatalf("err = %v, want ErrNotAttacking", err)
	}
	if !reflect.DeepEqual(c, before) {
		t.Fatalf("combat mutated: %+v", c)
	}
}

func TestCombat_DeclareBlocks_BeforeAttacks(t *testing.T) {
	var c Combat
	if err := c.DeclareBlocks(nil, allowAll, nil); !errors.Is(err, ErrAttacksNotDeclared) {
		t.Fatalf("err = %v", err)
	}
}

func TestCombat_DeclareBlocks_Twice(t *testing.T) {
	var c Combat
	declared(t, &c, 10)
	if err := c.DeclareBlocks(nil, allowAll, nil); err != nil {
		t.Fatal(err)
	}
	if err := c.DeclareBlocks([][2]EntityID{{1, 10}}, allowAll, nil); !errors.Is(err, ErrBlocksAlreadyDeclared) {
		t.Fatalf("err = %v", err)
	}
}

func TestCombat_DeclareBlocks_NotAttacking(t *testing.T) {
	var c Combat
	declared(t, &c, 10)
	before := c
	err := c.DeclareBlocks([][2]EntityID{{1, 99}}, allowAll, nil)
	if !errors.Is(err, ErrNotAttacking) {
		t.Fatalf("err = %v", err)
	}
	if !reflect.DeepEqual(c, before) {
		t.Fatalf("combat mutated: %+v", c)
	}
}

func TestCombat_DeclareBlocks_IllegalPair(t *testing.T) {
	var c Combat
	declared(t, &c, 10, 11)
	before := c
	legal := func(b, a EntityID) bool { return b != 2 }
	err := c.DeclareBlocks([][2]EntityID{{1, 10}, {2, 11}}, legal, nil)
	if !errors.Is(err, ErrIllegalBlock) {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), "blocker 2 -> attacker 11") {
		t.Fatalf("err %q lacks the rejected pair", err)
	}
	if !reflect.DeepEqual(c, before) {
		t.Fatalf("combat mutated: %+v", c)
	}
}

func TestCombat_LegalBlocks_DeterministicOrder(t *testing.T) {
	var c Combat
	declared(t, &c, 30, 10, 20)
	legal := func(b, a EntityID) bool { return !(b == 5 && a == 10) }
	got := c.LegalBlocks([]EntityID{7, 5, 6}, legal)
	// attack order is 30, 10, 20; blockers ascending within each.
	want := [][2]EntityID{
		{5, 30}, {6, 30}, {7, 30},
		{6, 10}, {7, 10},
		{5, 20}, {6, 20}, {7, 20},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestCombat_LegalBlocks_DuplicateCandidates(t *testing.T) {
	var c Combat
	declared(t, &c, 10, 11)
	calls := 0
	legal := func(b, a EntityID) bool { calls++; return true }
	got := c.LegalBlocks([]EntityID{6, 5, 6, 5, 5}, legal)
	want := [][2]EntityID{{5, 10}, {6, 10}, {5, 11}, {6, 11}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	if calls != 4 {
		t.Fatalf("legal called %d times, want once per distinct pair (4)", calls)
	}
}

// TestCombat_NilLegalAllowsAll: a nil predicate accepts every pair in
// both LegalBlocks and DeclareBlocks instead of panicking.
func TestCombat_NilLegalAllowsAll(t *testing.T) {
	var c Combat
	declared(t, &c, 10, 11)
	got := c.LegalBlocks([]EntityID{2, 1}, nil)
	want := [][2]EntityID{{1, 10}, {2, 10}, {1, 11}, {2, 11}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("LegalBlocks(nil) = %v want %v", got, want)
	}
	if err := c.DeclareBlocks([][2]EntityID{{1, 10}, {2, 11}}, nil, nil); err != nil {
		t.Fatalf("DeclareBlocks(nil legal): %v", err)
	}
	if !c.Blocked(10) || !c.Blocked(11) {
		t.Fatalf("blocks = %v", c.Blocks)
	}
}

func TestCombat_BlockRequest(t *testing.T) {
	var c Combat
	declared(t, &c, 10, 11)
	legal := func(b, a EntityID) bool { return !(b == 2 && a == 10) }
	req := c.BlockRequest("blocks", []EntityID{2, 1}, legal)
	want := core.AssignmentRequest{
		Kind:        "blocks",
		Pairs:       [][2]any{{EntityID(1), EntityID(10)}, {EntityID(1), EntityID(11)}, {EntityID(2), EntityID(11)}},
		MaxPerLeft:  1,
		MaxPerRight: 1,
		AllowEmpty:  true,
	}
	if !reflect.DeepEqual(req, want) {
		t.Fatalf("req = %+v\nwant %+v", req, want)
	}

	// The request carries the round's cap, so it never disagrees with
	// DeclareBlocks; 0 and negatives read as 1.
	for cfg, wantMax := range map[int]int{-2: 1, 0: 1, 1: 1, 3: 3} {
		c.MaxBlockersPerAttacker = cfg
		if got := c.BlockRequest("blocks", []EntityID{1}, nil).MaxPerRight; got != wantMax {
			t.Errorf("MaxBlockersPerAttacker %d: MaxPerRight = %d, want %d", cfg, got, wantMax)
		}
	}

	// No legal blocks still yields an answerable request.
	none := c.BlockRequest("blocks", nil, legal)
	if none.Pairs != nil || !none.AllowEmpty {
		t.Fatalf("empty request = %+v", none)
	}
	if _, err := core.ValidateAssignment(none, nil); err != nil {
		t.Fatalf("empty answer to an empty request: %v", err)
	}
}

// TestCombat_BlockRequest_AnswerFeedsDeclareBlocks walks the documented
// path in-process: request, validate, convert, declare.
func TestCombat_BlockRequest_AnswerFeedsDeclareBlocks(t *testing.T) {
	var c Combat
	declared(t, &c, 10, 11)
	c.MaxBlockersPerAttacker = 2
	req := c.BlockRequest("blocks", []EntityID{1, 2, 3}, allowAll)
	chosen, err := core.ValidateAssignment(req, []any{
		[2]any{EntityID(1), EntityID(10)}, [2]any{EntityID(2), EntityID(10)}, [2]any{EntityID(3), EntityID(11)},
	})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	pairs, err := EntityPairs(chosen)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if err := c.DeclareBlocks(pairs, allowAll, nil); err != nil {
		t.Fatalf("declare: %v", err)
	}
	if got := c.BlockersOf(10); !reflect.DeepEqual(got, []EntityID{1, 2}) {
		t.Fatalf("blockers of 10 = %v", got)
	}
	if got := c.BlockersOf(11); !reflect.DeepEqual(got, []EntityID{3}) {
		t.Fatalf("blockers of 11 = %v", got)
	}
}

func TestCombat_DeclareBlocks_BlockerReused(t *testing.T) {
	var c Combat
	declared(t, &c, 10, 11)
	before := c
	err := c.DeclareBlocks([][2]EntityID{{1, 10}, {1, 11}}, allowAll, nil)
	if !errors.Is(err, ErrBlockerReused) {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), "blocker 1") {
		t.Fatalf("err %q lacks the reused blocker", err)
	}
	if !reflect.DeepEqual(c, before) {
		t.Fatalf("combat mutated: %+v", c)
	}
}

func TestCombat_DeclareBlocks_MaxBlockersDefaultOne(t *testing.T) {
	var c Combat
	declared(t, &c, 10)
	pairs := [][2]EntityID{{1, 10}, {2, 10}}
	before := c
	if err := c.DeclareBlocks(pairs, allowAll, nil); !errors.Is(err, ErrTooManyBlockers) {
		t.Fatalf("err = %v", err)
	}
	if !reflect.DeepEqual(c, before) {
		t.Fatalf("combat mutated: %+v", c)
	}
	c.MaxBlockersPerAttacker = -1 // negative reads as 1, like 0
	if err := c.DeclareBlocks(pairs, allowAll, nil); !errors.Is(err, ErrTooManyBlockers) {
		t.Fatalf("negative cap: err = %v", err)
	}
	c.MaxBlockersPerAttacker = 2
	if err := c.DeclareBlocks(pairs, allowAll, nil); err != nil {
		t.Fatal(err)
	}
	if got := c.BlockersOf(10); !reflect.DeepEqual(got, []EntityID{1, 2}) {
		t.Fatalf("blockers = %v", got)
	}
}

func TestCombat_DeclareBlocks_WholeHookRejects(t *testing.T) {
	var c Combat
	declared(t, &c, 10, 11)
	c.MaxBlockersPerAttacker = 2
	before := c
	need := errors.New("needs two blockers")
	var seen Combat
	whole := func(p Combat) error {
		seen = p
		for _, a := range p.Attacks {
			if p.Blocked(a.Attacker) && len(p.BlockersOf(a.Attacker)) < 2 {
				return need
			}
		}
		return nil
	}
	err := c.DeclareBlocks([][2]EntityID{{1, 10}}, allowAll, whole)
	if !errors.Is(err, need) {
		t.Fatalf("err = %v", err)
	}
	// The hook saw the round as it would be after the declaration.
	want := before
	want.Blocks = map[EntityID][]EntityID{10: {1}}
	want.BlocksDeclared = true
	if !reflect.DeepEqual(seen, want) {
		t.Fatalf("hook saw %+v\nwant %+v", seen, want)
	}
	if got := seen.Unblocked(); len(got) != 1 || got[0].Attacker != 11 {
		t.Fatalf("hook's Unblocked = %v", got)
	}
	if !reflect.DeepEqual(c, before) {
		t.Fatalf("combat mutated: %+v", c)
	}
}

// TestCombat_DeclareBlocks_WholeHookGetsDeepCopy: whatever the hook does
// to its argument, the committed round is the declaration itself.
func TestCombat_DeclareBlocks_WholeHookGetsDeepCopy(t *testing.T) {
	var c Combat
	declared(t, &c, 10, 11)
	c.MaxBlockersPerAttacker = 2
	whole := func(p Combat) error {
		p.Blocks[10][0] = 99
		p.Blocks[10] = append(p.Blocks[10], 98)
		p.Blocks[11] = []EntityID{97}
		p.Attacks[0].Attacker = 96
		p.Attacks = append(p.Attacks, Attack{Attacker: 95})
		p.MaxBlockersPerAttacker = 9
		return nil
	}
	if err := c.DeclareBlocks([][2]EntityID{{1, 10}, {2, 10}}, allowAll, whole); err != nil {
		t.Fatal(err)
	}
	want := Combat{
		Attacks:                []Attack{{Attacker: 10, Target: CombatTarget{Player: "1"}}, {Attacker: 11, Target: CombatTarget{Player: "1"}}},
		AttacksDeclared:        true,
		Blocks:                 map[EntityID][]EntityID{10: {1, 2}},
		BlocksDeclared:         true,
		MaxBlockersPerAttacker: 2,
	}
	if !reflect.DeepEqual(c, want) {
		t.Fatalf("hook's edits leaked:\n got  %+v\n want %+v", c, want)
	}
}

func TestCombat_DeclareBlocks_EmptyMarksDeclared(t *testing.T) {
	var c Combat
	declared(t, &c, 10, 11)
	if got := c.Unblocked(); len(got) != 0 {
		t.Fatalf("unblocked before declaration = %v", got)
	}
	if err := c.DeclareBlocks(nil, allowAll, nil); err != nil {
		t.Fatal(err)
	}
	if !c.BlocksDeclared {
		t.Fatal("BlocksDeclared = false")
	}
	if got := c.Unblocked(); !reflect.DeepEqual(got, c.Attacks) || len(got) != 2 {
		t.Fatalf("unblocked = %v", got)
	}
}

func TestCombat_Queries(t *testing.T) {
	var c Combat
	declared(t, &c, 10, 11, 12)
	if err := c.DeclareBlocks([][2]EntityID{{1, 11}}, allowAll, nil); err != nil {
		t.Fatal(err)
	}
	if !c.Blocked(11) || c.Blocked(10) {
		t.Fatal("Blocked wrong")
	}
	if a, ok := c.Blocking(1); !ok || a != 11 {
		t.Fatalf("Blocking(1) = %v, %v", a, ok)
	}
	if _, ok := c.Blocking(2); ok {
		t.Fatal("Blocking(2) should be false")
	}
	got := c.Unblocked()
	if len(got) != 2 || got[0].Attacker != 10 || got[1].Attacker != 12 {
		t.Fatalf("unblocked = %v", got)
	}
}

func TestCombat_Remove_BlockerKeepsAttackerBlocked(t *testing.T) {
	var c Combat
	declared(t, &c, 10)
	if err := c.DeclareBlocks([][2]EntityID{{1, 10}}, allowAll, nil); err != nil {
		t.Fatal(err)
	}
	c.Remove(1)
	if !c.Blocked(10) {
		t.Fatal("attacker should stay blocked")
	}
	if len(c.BlockersOf(10)) != 0 {
		t.Fatalf("blockers = %v", c.BlockersOf(10))
	}
	if _, ok := c.Blocking(1); ok {
		t.Fatal("removed blocker still blocking")
	}
	if len(c.Unblocked()) != 0 {
		t.Fatalf("unblocked = %v", c.Unblocked())
	}
}

func TestCombat_Remove_OneOfSeveralBlockers(t *testing.T) {
	c := Combat{MaxBlockersPerAttacker: 3}
	declared(t, &c, 10, 11)
	if err := c.DeclareBlocks([][2]EntityID{{1, 10}, {2, 10}, {3, 10}, {4, 11}}, allowAll, nil); err != nil {
		t.Fatal(err)
	}
	c.Remove(2)
	if got := c.BlockersOf(10); !reflect.DeepEqual(got, []EntityID{1, 3}) {
		t.Fatalf("blockers of 10 = %v, want [1 3] in declaration order", got)
	}
	if got := c.BlockersOf(11); !reflect.DeepEqual(got, []EntityID{4}) {
		t.Fatalf("blockers of 11 = %v", got)
	}
	if _, ok := c.Blocking(2); ok {
		t.Fatal("removed blocker still blocking")
	}
	if a, ok := c.Blocking(3); !ok || a != 10 {
		t.Fatalf("Blocking(3) = %v, %v", a, ok)
	}
	if !c.Blocked(10) {
		t.Fatal("attacker 10 should stay blocked")
	}
}

// TestCombat_Remove_LeavesTargetsAlone pins the documented behavior:
// removing an entity does not rewrite attacks aimed at it.
func TestCombat_Remove_LeavesTargetsAlone(t *testing.T) {
	var c Combat
	if err := c.DeclareAttacks([]Attack{{Attacker: 10, Target: CombatTarget{Entity: 20}}}, nil); err != nil {
		t.Fatal(err)
	}
	c.Remove(20)
	if len(c.Attacks) != 1 || c.Attacks[0].Target.Entity != 20 {
		t.Fatalf("attacks = %+v", c.Attacks)
	}
}

func TestCombat_Remove_Attacker(t *testing.T) {
	var c Combat
	declared(t, &c, 10, 11)
	if err := c.DeclareBlocks([][2]EntityID{{1, 10}}, allowAll, nil); err != nil {
		t.Fatal(err)
	}
	c.Remove(10)
	if c.Attacking(10) || !c.Attacking(11) {
		t.Fatalf("attacks = %+v", c.Attacks)
	}
	if _, ok := c.Blocks[10]; ok {
		t.Fatal("Blocks key remains")
	}
	if _, ok := c.Blocking(1); ok {
		t.Fatal("blocker of removed attacker still blocking")
	}
}

func TestCombat_JSONRoundTrip(t *testing.T) {
	var c Combat
	declared(t, &c, 10, 11)
	if err := c.DeclareBlocks([][2]EntityID{{1, 10}, {2, 11}}, allowAll, nil); err != nil {
		t.Fatal(err)
	}
	c.Remove(2) // 11 is now blocked with an empty blocker slice
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var back Combat
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c, back) {
		t.Fatalf("round trip: %+v != %+v", back, c)
	}
	if !back.Blocked(11) || len(back.BlockersOf(11)) != 0 || !back.Blocked(10) {
		t.Fatal("blocked state lost")
	}
}

func TestCombat_Reset(t *testing.T) {
	var c Combat
	declared(t, &c, 10)
	if err := c.DeclareBlocks([][2]EntityID{{1, 10}}, allowAll, nil); err != nil {
		t.Fatal(err)
	}
	c.Reset()
	if !reflect.DeepEqual(c, Combat{}) {
		t.Fatalf("not zero: %+v", c)
	}
}

func TestCombat_Reset_KeepsConfig(t *testing.T) {
	c := Combat{MaxBlockersPerAttacker: 3}
	declared(t, &c, 10)
	c.Reset()
	if c.MaxBlockersPerAttacker != 3 || len(c.Attacks) != 0 {
		t.Fatalf("after reset: %+v", c)
	}
}
