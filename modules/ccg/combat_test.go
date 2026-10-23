package ccg

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
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
	err := c.DeclareAttacks([]Attack{{Attacker: 1}, {Attacker: 1}}, nil)
	if !errors.Is(err, ErrDuplicateAttacker) {
		t.Fatalf("err = %v", err)
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

func TestCombat_DeclareBlocks_BlockerReused(t *testing.T) {
	var c Combat
	declared(t, &c, 10, 11)
	before := c
	err := c.DeclareBlocks([][2]EntityID{{1, 10}, {1, 11}}, allowAll, nil)
	if !errors.Is(err, ErrBlockerReused) {
		t.Fatalf("err = %v", err)
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
	var seen map[EntityID][]EntityID
	whole := func(m map[EntityID][]EntityID) error {
		seen = m
		for _, bs := range m {
			if len(bs) < 2 {
				return need
			}
		}
		return nil
	}
	err := c.DeclareBlocks([][2]EntityID{{1, 10}}, allowAll, whole)
	if !errors.Is(err, need) {
		t.Fatalf("err = %v", err)
	}
	if !reflect.DeepEqual(seen, map[EntityID][]EntityID{10: {1}}) {
		t.Fatalf("hook saw %v", seen)
	}
	if !reflect.DeepEqual(c, before) {
		t.Fatalf("combat mutated: %+v", c)
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
