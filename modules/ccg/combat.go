package ccg

import (
	"errors"
	"sort"
)

// Combat declaration: who attacks what, who blocks whom.
//
// Combat is serializable bookkeeping for the declaration half of a
// combat round: the attackers and their targets, the blockers
// assigned to each attacker, and which attackers went unblocked. It
// lives in the game's G and serializes with it.
//
// It records and validates structure only. Which attacks and blocks
// are allowed is decided by predicates the game passes in, and what
// damage means (who deals it, how it is split among several blockers,
// what an unblocked attacker does) stays entirely in the game.
//
// Block declaration takes a whole-assignment hook for constraints
// that cannot be judged one pair at a time, such as "this attacker
// must be blocked by two or more creatures" or "if X blocks, Y must
// too". The hook sees the complete attacker -> blockers map and
// returns an error to reject the declaration.
//
// LegalBlocks enumerates the legal (blocker, attacker) pairs in a
// deterministic order; its output can be passed directly as
// core.AssignmentRequest.Pairs, and the player's answer fed back
// into DeclareBlocks.

var (
	// ErrNoAttacks is returned by DeclareAttacks given no attacks.
	ErrNoAttacks = errors.New("ccg: no attacks declared")
	// ErrDuplicateAttacker is returned when one entity is declared as
	// an attacker twice.
	ErrDuplicateAttacker = errors.New("ccg: duplicate attacker")
	// ErrAttacksAlreadyDeclared is returned by DeclareAttacks once
	// attacks have been declared (until Reset).
	ErrAttacksAlreadyDeclared = errors.New("ccg: attacks already declared")
	// ErrAttacksNotDeclared is returned by DeclareBlocks before any
	// attacks exist.
	ErrAttacksNotDeclared = errors.New("ccg: attacks not declared")
	// ErrBlocksAlreadyDeclared is returned by DeclareBlocks once blocks
	// have been declared (until Reset).
	ErrBlocksAlreadyDeclared = errors.New("ccg: blocks already declared")
	// ErrNotAttacking is returned when a block names an entity that is
	// not a declared attacker.
	ErrNotAttacking = errors.New("ccg: entity is not attacking")
	// ErrBlockerReused is returned when one entity blocks more than
	// one attacker in a single declaration.
	ErrBlockerReused = errors.New("ccg: blocker assigned more than once")
	// ErrTooManyBlockers is returned when an attacker is given more
	// blockers than MaxBlockersPerAttacker allows.
	ErrTooManyBlockers = errors.New("ccg: too many blockers for attacker")
	// ErrIllegalBlock is returned when the legality predicate rejects
	// a (blocker, attacker) pair.
	ErrIllegalBlock = errors.New("ccg: illegal block")
)

// CombatTarget is what an attack is aimed at: a player, or an entity.
// Exactly which field is set is the game's convention; Combat does
// not interpret it.
type CombatTarget struct {
	Player string   `json:"player,omitempty"`
	Entity EntityID `json:"entity,omitempty"`
}

// Attack is one declared attacker and its target.
type Attack struct {
	Attacker EntityID     `json:"attacker"`
	Target   CombatTarget `json:"target"`
}

// Combat is the declaration state of one combat round. The zero value
// is an empty round.
type Combat struct {
	// Attacks are the declared attacks, in declaration order.
	Attacks []Attack `json:"attacks,omitempty"`
	// Blocks maps attacker -> blockers. A present key means the
	// attacker is blocked, even when its slice is empty (every blocker
	// was later removed).
	Blocks map[EntityID][]EntityID `json:"blocks,omitempty"`
	// BlocksDeclared is true once DeclareBlocks has succeeded, even
	// with no blocks.
	BlocksDeclared bool `json:"blocks_declared,omitempty"`
	// MaxBlockersPerAttacker caps blockers per attacker; 0 means 1.
	// It is configuration: Reset keeps it.
	MaxBlockersPerAttacker int `json:"max_blockers_per_attacker,omitempty"`
}

// DeclareAttacks records the attacks. legal, if non-nil, is called per
// attack and its error aborts the declaration. All-or-nothing: on any
// error the Combat is unchanged.
func (c *Combat) DeclareAttacks(attacks []Attack, legal func(Attack) error) error {
	if len(c.Attacks) > 0 {
		return ErrAttacksAlreadyDeclared
	}
	if len(attacks) == 0 {
		return ErrNoAttacks
	}
	seen := make(map[EntityID]bool, len(attacks))
	for _, a := range attacks {
		if seen[a.Attacker] {
			return ErrDuplicateAttacker
		}
		seen[a.Attacker] = true
		if legal != nil {
			if err := legal(a); err != nil {
				return err
			}
		}
	}
	c.Attacks = append([]Attack(nil), attacks...)
	return nil
}

// Attacking reports whether id is a declared attacker.
func (c *Combat) Attacking(id EntityID) bool {
	for _, a := range c.Attacks {
		if a.Attacker == id {
			return true
		}
	}
	return false
}

// LegalBlocks returns every (blocker, attacker) pair that legal
// accepts. Order is deterministic and independent of the order of
// candidates: attacks in declaration order, then blocker id ascending.
// The result can be used as core.AssignmentRequest.Pairs.
func (c *Combat) LegalBlocks(candidates []EntityID, legal func(blocker, attacker EntityID) bool) [][2]EntityID {
	sorted := append([]EntityID(nil), candidates...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	var out [][2]EntityID
	for _, a := range c.Attacks {
		var prev EntityID
		for i, b := range sorted {
			if i > 0 && b == prev {
				continue // duplicate candidate
			}
			prev = b
			if legal(b, a.Attacker) {
				out = append(out, [2]EntityID{b, a.Attacker})
			}
		}
	}
	return out
}

// DeclareBlocks records blocks given as (blocker, attacker) pairs.
// Empty pairs is a valid declaration of no blocks. legal judges each
// pair (ErrIllegalBlock on false). whole, if non-nil, sees the
// resulting attacker -> blockers map (blocked attackers only) and may
// reject it; this is where whole-assignment constraints plug in. The
// map passed to whole must not be retained or modified. All-or-nothing:
// on any error the Combat is unchanged.
func (c *Combat) DeclareBlocks(pairs [][2]EntityID, legal func(blocker, attacker EntityID) bool,
	whole func(map[EntityID][]EntityID) error) error {
	if len(c.Attacks) == 0 {
		return ErrAttacksNotDeclared
	}
	if c.BlocksDeclared {
		return ErrBlocksAlreadyDeclared
	}
	max := c.MaxBlockersPerAttacker
	if max <= 0 {
		max = 1
	}
	used := make(map[EntityID]bool, len(pairs))
	blocks := make(map[EntityID][]EntityID)
	for _, p := range pairs {
		blocker, attacker := p[0], p[1]
		if !c.Attacking(attacker) {
			return ErrNotAttacking
		}
		if used[blocker] {
			return ErrBlockerReused
		}
		used[blocker] = true
		if legal != nil && !legal(blocker, attacker) {
			return ErrIllegalBlock
		}
		if len(blocks[attacker]) >= max {
			return ErrTooManyBlockers
		}
		blocks[attacker] = append(blocks[attacker], blocker)
	}
	if whole != nil {
		if err := whole(blocks); err != nil {
			return err
		}
	}
	if len(blocks) > 0 {
		c.Blocks = blocks
	}
	c.BlocksDeclared = true
	return nil
}

// Blocked reports whether the attacker has a Blocks entry. It stays
// true after all its blockers are removed.
func (c *Combat) Blocked(attacker EntityID) bool {
	_, ok := c.Blocks[attacker]
	return ok
}

// BlockersOf returns a copy of the attacker's current blockers.
func (c *Combat) BlockersOf(attacker EntityID) []EntityID {
	return append([]EntityID(nil), c.Blocks[attacker]...)
}

// Blocking returns the attacker that id is blocking, if any.
func (c *Combat) Blocking(id EntityID) (attacker EntityID, ok bool) {
	for _, a := range c.Attacks {
		for _, b := range c.Blocks[a.Attacker] {
			if b == id {
				return a.Attacker, true
			}
		}
	}
	return 0, false
}

// Unblocked returns the attacks with no Blocks entry, in declaration
// order. It is empty until BlocksDeclared, since before then nothing
// is known to be unblocked.
func (c *Combat) Unblocked() []Attack {
	if !c.BlocksDeclared {
		return nil
	}
	var out []Attack
	for _, a := range c.Attacks {
		if !c.Blocked(a.Attacker) {
			out = append(out, a)
		}
	}
	return out
}

// Remove drops an entity from the round, for example when it dies. An
// attacker is removed along with its Blocks entry. A blocker is removed
// from its attacker's list; that attacker stays Blocked.
func (c *Combat) Remove(id EntityID) {
	for i, a := range c.Attacks {
		if a.Attacker == id {
			c.Attacks = append(c.Attacks[:i:i], c.Attacks[i+1:]...)
			delete(c.Blocks, id)
			break
		}
	}
	for atk, bs := range c.Blocks {
		for i, b := range bs {
			if b == id {
				c.Blocks[atk] = append(bs[:i:i], bs[i+1:]...)
				break
			}
		}
	}
}

// Reset clears the round for reuse. MaxBlockersPerAttacker, being
// configuration, is kept.
func (c *Combat) Reset() {
	*c = Combat{MaxBlockersPerAttacker: c.MaxBlockersPerAttacker}
}
