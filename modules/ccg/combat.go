package ccg

import (
	"errors"
	"fmt"
	"sort"

	"github.com/tjcran/boardgame-go/core"
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
// too". The hook receives a deep copy of the Combat as it would be
// after the declaration and returns an error to reject it.
//
// BlockRequest turns the legal blocks into the core.AssignmentRequest
// a defender answers; the resume move validates the answer with
// core.ValidateAssignment, converts it with EntityPairs, and feeds it
// into DeclareBlocks.

var (
	// ErrNoAttacks is returned by DeclareAttacks given no attacks.
	ErrNoAttacks = errors.New("ccg: no attacks declared")
	// ErrDuplicateAttacker is returned when one entity is declared as
	// an attacker twice.
	ErrDuplicateAttacker = errors.New("ccg: duplicate attacker")
	// ErrAttacksAlreadyDeclared is returned by DeclareAttacks once
	// attacks have been declared (until Reset), even if every attacker
	// has since been removed.
	ErrAttacksAlreadyDeclared = errors.New("ccg: attacks already declared")
	// ErrAttacksNotDeclared is returned by DeclareBlocks before
	// DeclareAttacks has succeeded.
	ErrAttacksNotDeclared = errors.New("ccg: attacks not declared")
	// ErrBlocksAlreadyDeclared is returned by DeclareBlocks once blocks
	// have been declared (until Reset).
	ErrBlocksAlreadyDeclared = errors.New("ccg: blocks already declared")
	// ErrNotAttacking is returned when a block names an entity that is
	// not a declared attacker.
	ErrNotAttacking = errors.New("ccg: entity is not attacking")
	// ErrBlockerReused is returned when one entity blocks more than
	// one attacker in a single declaration: a blocker blocks exactly
	// one attacker.
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
// not interpret it. Entity 0 means "no entity": State.NewEntity mints
// IDs from 1, and omitempty drops a 0 from the JSON.
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
//
// For anything order-dependent, range over Attacks (declaration order)
// and read each attacker's blockers with BlockersOf (declaration
// order), never over the Blocks map, whose iteration order is random.
type Combat struct {
	// Attacks are the declared attacks, in declaration order.
	Attacks []Attack `json:"attacks,omitempty"`
	// AttacksDeclared is true once DeclareAttacks has succeeded. It
	// stays true when Remove takes out every attacker, so the round
	// can still close with DeclareBlocks(nil, …) and attacks cannot be
	// declared twice.
	AttacksDeclared bool `json:"attacks_declared,omitempty"`
	// Blocks maps attacker -> blockers, each list in declaration
	// order. A present key means the attacker is blocked, even when
	// its slice is empty (every blocker was later removed).
	Blocks map[EntityID][]EntityID `json:"blocks,omitempty"`
	// BlocksDeclared is true once DeclareBlocks has succeeded, even
	// with no blocks.
	BlocksDeclared bool `json:"blocks_declared,omitempty"`
	// MaxBlockersPerAttacker caps blockers per attacker; 0 or a
	// negative value means 1. It is configuration: Reset keeps it.
	MaxBlockersPerAttacker int `json:"max_blockers_per_attacker,omitempty"`
}

// maxBlockers is MaxBlockersPerAttacker with 0 and negatives read as 1.
func (c *Combat) maxBlockers() int {
	if c.MaxBlockersPerAttacker <= 0 {
		return 1
	}
	return c.MaxBlockersPerAttacker
}

// DeclareAttacks records the attacks. legal, if non-nil, is called per
// attack and its error aborts the declaration. All-or-nothing: on any
// error the Combat is unchanged.
func (c *Combat) DeclareAttacks(attacks []Attack, legal func(Attack) error) error {
	if c.AttacksDeclared {
		return ErrAttacksAlreadyDeclared
	}
	if len(attacks) == 0 {
		return ErrNoAttacks
	}
	seen := make(map[EntityID]bool, len(attacks))
	for _, a := range attacks {
		if seen[a.Attacker] {
			return fmt.Errorf("%w: attacker %d", ErrDuplicateAttacker, a.Attacker)
		}
		seen[a.Attacker] = true
		if legal != nil {
			if err := legal(a); err != nil {
				return err
			}
		}
	}
	c.Attacks = append([]Attack(nil), attacks...)
	c.AttacksDeclared = true
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

// LegalBlocks returns every (blocker, attacker) pair that legal, if
// non-nil, accepts; a nil legal accepts every pair. Order is
// deterministic and independent of the order of candidates: attacks in
// declaration order, then blocker id ascending. A candidate listed
// more than once is considered once.
//
// To offer the pairs to a player, use BlockRequest, which converts them
// with ToAssignmentPairs and sets the per-item limits.
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
			if legal == nil || legal(b, a.Attacker) {
				out = append(out, [2]EntityID{b, a.Attacker})
			}
		}
	}
	return out
}

// BlockRequest builds the core.AssignmentRequest that asks a defender
// to declare blocks: Pairs are LegalBlocks(candidates, legal) as
// (blocker, attacker) pairs, MaxPerLeft is 1 because a blocker blocks
// exactly one attacker, MaxPerRight is the round's
// MaxBlockersPerAttacker (0 or negative read as 1), and AllowEmpty is
// true because declaring no blocks is always legal, which also keeps a
// round with no legal blocks answerable.
//
// Raise it with core.Queue.RequestAssignment. In the resume move,
// validate the args with core.ValidateAssignment, convert the result
// with EntityPairs, and pass it to DeclareBlocks with the same legal
// predicate; DeclareBlocks re-checks every rule, so a request that has
// gone stale since it was raised cannot record an illegal block.
func (c *Combat) BlockRequest(kind string, candidates []EntityID, legal func(blocker, attacker EntityID) bool) core.AssignmentRequest {
	return core.AssignmentRequest{
		Kind:        kind,
		Pairs:       ToAssignmentPairs(c.LegalBlocks(candidates, legal)),
		MaxPerLeft:  1,
		MaxPerRight: c.maxBlockers(),
		AllowEmpty:  true,
	}
}

// DeclareBlocks records blocks given as (blocker, attacker) pairs.
// Empty pairs is a valid declaration of no blocks. legal, if non-nil,
// judges each pair (ErrIllegalBlock on false); a nil legal accepts
// every pair. Each blocker may appear in one pair only
// (ErrBlockerReused), and each attacker may take up to
// MaxBlockersPerAttacker blockers (ErrTooManyBlockers).
//
// whole, if non-nil, is the whole-assignment hook: it receives a deep
// copy of the Combat as it would be after the declaration
// (BlocksDeclared true, Blocks set) and may reject it by returning an
// error, which DeclareBlocks returns unchanged. Read the copy through
// its deterministic accessors (Attacks, BlockersOf, Blocked,
// Unblocked); changes to it have no effect.
//
// All-or-nothing: on any error the Combat is unchanged. Errors for a
// specific pair wrap their sentinel with the entities involved, so
// test them with errors.Is.
func (c *Combat) DeclareBlocks(pairs [][2]EntityID, legal func(blocker, attacker EntityID) bool,
	whole func(proposed Combat) error) error {
	if !c.AttacksDeclared {
		return ErrAttacksNotDeclared
	}
	if c.BlocksDeclared {
		return ErrBlocksAlreadyDeclared
	}
	max := c.maxBlockers()
	used := make(map[EntityID]bool, len(pairs))
	blocks := make(map[EntityID][]EntityID)
	for _, p := range pairs {
		blocker, attacker := p[0], p[1]
		if !c.Attacking(attacker) {
			return fmt.Errorf("%w: attacker %d (blocker %d)", ErrNotAttacking, attacker, blocker)
		}
		if used[blocker] {
			return fmt.Errorf("%w: blocker %d", ErrBlockerReused, blocker)
		}
		used[blocker] = true
		if legal != nil && !legal(blocker, attacker) {
			return fmt.Errorf("%w: blocker %d -> attacker %d", ErrIllegalBlock, blocker, attacker)
		}
		if len(blocks[attacker]) >= max {
			return fmt.Errorf("%w: attacker %d, max %d", ErrTooManyBlockers, attacker, max)
		}
		blocks[attacker] = append(blocks[attacker], blocker)
	}
	if whole != nil {
		proposed := c.clone()
		if len(blocks) > 0 {
			proposed.Blocks = cloneBlocks(blocks)
		}
		proposed.BlocksDeclared = true
		if err := whole(proposed); err != nil {
			return err
		}
	}
	if len(blocks) > 0 {
		c.Blocks = blocks
	}
	c.BlocksDeclared = true
	return nil
}

// clone returns a deep copy: no slice or map is shared with c.
func (c *Combat) clone() Combat {
	out := *c
	out.Attacks = append([]Attack(nil), c.Attacks...)
	out.Blocks = cloneBlocks(c.Blocks)
	return out
}

// cloneBlocks deep-copies a Blocks map, keeping nil and empty slices
// distinct so a copy encodes to the same JSON.
func cloneBlocks(m map[EntityID][]EntityID) map[EntityID][]EntityID {
	if m == nil {
		return nil
	}
	out := make(map[EntityID][]EntityID, len(m))
	for k, v := range m {
		if v != nil {
			v = append(make([]EntityID, 0, len(v)), v...)
		}
		out[k] = v
	}
	return out
}

// Blocked reports whether the attacker has a Blocks entry. It stays
// true after all its blockers are removed.
func (c *Combat) Blocked(attacker EntityID) bool {
	_, ok := c.Blocks[attacker]
	return ok
}

// BlockersOf returns a copy of the attacker's current blockers, in
// declaration order.
func (c *Combat) BlockersOf(attacker EntityID) []EntityID {
	return append([]EntityID(nil), c.Blocks[attacker]...)
}

// Blocking returns the attacker that id is blocking, if any. A blocker
// blocks exactly one attacker, so there is at most one answer.
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
//
// Remove does not touch attack targets: an Attack whose Target.Entity
// is id keeps pointing at it, and what an attack on a removed entity
// does is the game's call. Removing every attacker leaves
// AttacksDeclared true, so attacks cannot be declared again before
// Reset, and DeclareBlocks(nil, …) still closes the round.
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
