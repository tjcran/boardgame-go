package ccg

import (
	"errors"
	"fmt"
	"sort"
)

// DeckList expresses a deck as a count of each card def. The library
// treats it as pure data — game authors validate, load into a zone,
// and shuffle separately.
//
// DeckList is a map for ergonomic authoring (`{ "goblin": 4 }`); methods
// on it return deterministically-ordered slices so iteration is
// replay-safe.
type DeckList map[DefID]int

// Total returns the sum of card counts in the list.
func (d DeckList) Total() int {
	n := 0
	for _, c := range d {
		n += c
	}
	return n
}

// Cards returns every DefID in the list, repeated by count, in a
// deterministic order: DefIDs sorted lexicographically, each repeated
// `count` times. LoadDeckList uses the same ordering so a given list
// always produces the same entity-ID sequence on replay.
func (d DeckList) Cards() []DefID {
	keys := make([]DefID, 0, len(d))
	for k := range d {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	out := make([]DefID, 0, d.Total())
	for _, k := range keys {
		for i := 0; i < d[k]; i++ {
			out = append(out, k)
		}
	}
	return out
}

// DeckValidator is the interface for composable deck validators.
// Validate returns nil when the list is legal for this validator's
// rules, or a descriptive error.
type DeckValidator interface {
	Validate(DeckList, *Catalog) error
}

// ErrDeckList categories. Specific validators wrap these with context
// (e.g., MaxCopies returns `fmt.Errorf("ccg: too many copies of %s: %w",
// defID, ErrDeckListMaxCopies)`).
var (
	ErrDeckListMinSize     = errors.New("ccg: deck below minimum size")
	ErrDeckListMaxSize     = errors.New("ccg: deck above maximum size")
	ErrDeckListMaxCopies   = errors.New("ccg: too many copies of a card")
	ErrDeckListUnknownDef  = errors.New("ccg: deck references unknown def")
	ErrDeckListNegativeCnt = errors.New("ccg: deck has non-positive card count")
)

type minSizeValidator struct{ n int }

func (v minSizeValidator) Validate(d DeckList, _ *Catalog) error {
	if d.Total() < v.n {
		return fmt.Errorf("%w: have %d, need ≥ %d", ErrDeckListMinSize, d.Total(), v.n)
	}
	return nil
}

// MinSize returns a validator requiring at least n cards total.
func MinSize(n int) DeckValidator { return minSizeValidator{n: n} }

type maxSizeValidator struct{ n int }

func (v maxSizeValidator) Validate(d DeckList, _ *Catalog) error {
	if d.Total() > v.n {
		return fmt.Errorf("%w: have %d, allow ≤ %d", ErrDeckListMaxSize, d.Total(), v.n)
	}
	return nil
}

// MaxSize returns a validator requiring at most n cards total.
func MaxSize(n int) DeckValidator { return maxSizeValidator{n: n} }

type maxCopiesValidator struct{ n int }

func (v maxCopiesValidator) Validate(d DeckList, _ *Catalog) error {
	for _, id := range sortedKeys(d) {
		if d[id] > v.n {
			return fmt.Errorf("%w: %s has %d copies, allow ≤ %d", ErrDeckListMaxCopies, id, d[id], v.n)
		}
	}
	return nil
}

// MaxCopies returns a validator requiring at most n copies of any single
// def. Walks defs in deterministic order so the first reported failure
// is stable across runs.
func MaxCopies(n int) DeckValidator { return maxCopiesValidator{n: n} }

type requireDefsExistValidator struct{}

func (requireDefsExistValidator) Validate(d DeckList, c *Catalog) error {
	if c == nil {
		return ErrDeckListUnknownDef
	}
	for _, id := range sortedKeys(d) {
		if _, ok := c.Get(id); !ok {
			return fmt.Errorf("%w: %s", ErrDeckListUnknownDef, id)
		}
	}
	return nil
}

// RequireDefsExist returns a validator that fails if any DefID in the
// list isn't registered in the catalog.
func RequireDefsExist() DeckValidator { return requireDefsExistValidator{} }

type nonNegativeCountsValidator struct{}

func (nonNegativeCountsValidator) Validate(d DeckList, _ *Catalog) error {
	for _, id := range sortedKeys(d) {
		if d[id] <= 0 {
			return fmt.Errorf("%w: %s has count %d", ErrDeckListNegativeCnt, id, d[id])
		}
	}
	return nil
}

// NonNegativeCounts returns a validator that rejects zero or negative
// counts. LoadDeckList tolerates zero entries (treats them as absent)
// but explicit validators usually want to flag them.
func NonNegativeCounts() DeckValidator { return nonNegativeCountsValidator{} }

type composedValidator struct{ vs []DeckValidator }

func (c composedValidator) Validate(d DeckList, cat *Catalog) error {
	for _, v := range c.vs {
		if err := v.Validate(d, cat); err != nil {
			return err
		}
	}
	return nil
}

// Compose runs the given validators in order, returning the first
// error or nil when all pass.
func Compose(vs ...DeckValidator) DeckValidator {
	return composedValidator{vs: vs}
}

// LoadDeckList instantiates one Entity per card in the list and adds
// them to the given zone in DeckList.Cards() order (defs sorted
// lexicographically, each repeated `count` times). This makes the
// resulting zone's Members slice deterministic for a given input,
// which matters for replay safety; shuffling is a separate explicit
// step the caller controls.
//
// Returns the slice of minted EntityIDs in insertion order. If
// instantiation or the zone Add fails partway through, already-added
// entities are left in place (the partial state is observable); the
// caller is expected to validate the list first and ensure zone
// capacity. Use `DeckList{...}.Total() <= zone.Capacity` as a guard,
// or run validators with `MaxSize(zone.Capacity)`.
//
// Entries with count <= 0 are skipped. Pair with NonNegativeCounts
// if you want them treated as errors instead.
func (s *State) LoadDeckList(c *Catalog, list DeckList, owner string, zone ZoneName) ([]EntityID, error) {
	if c == nil {
		return nil, ErrUnknownDef
	}
	cards := list.Cards()
	out := make([]EntityID, 0, len(cards))
	for _, defID := range cards {
		id, err := s.Instantiate(c, defID, owner)
		if err != nil {
			return out, err
		}
		if err := s.Add(zone, id); err != nil {
			return out, err
		}
		out = append(out, id)
	}
	return out, nil
}

func sortedKeys(d DeckList) []DefID {
	keys := make([]DefID, 0, len(d))
	for k := range d {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	return keys
}
