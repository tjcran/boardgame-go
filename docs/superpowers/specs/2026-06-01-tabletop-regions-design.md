# tabletop.Regions — design

**Status:** spec, awaiting review
**Driven by:** module coverage audit (2026-05-31), area-control gap
**Module:** extension to `modules/tabletop`

## Context

The module coverage audit identified one real gap in the engine's
module set: **area-control games**. `tabletop.Board` already gives
hex/square geometry (`Neighbors`, `Distance`, `Line`), and
`tabletop.State` gives unit positions. What it doesn't give is the
concept that area-control games rest on:

- A board is partitioned into *named regions* — groups of cells, not
  just neighborhoods.
- Players have *influence* in a region, computed from the units they
  own that sit in its cells.
- Scoring rounds award points by majority / plurality / top-N over
  that influence.

Every area-control game ends up re-implementing the same scaffolding:
a `Regions map[ID]struct{ Cells []Pos; Owner string }` field on `G`,
an `influenceFor(playerID, regionID)` helper, and a per-game `score()`
that handles ties some way. The audit verdict was that this is the
strongest module candidate in the engine.

This spec defines a `modules/tabletop/regions` extension (or
`tabletop.Map` if it stays in-package — see "Packaging") at **standard
scope** — enough surface to express El Grande, Blood Rage, Inis,
Risk-with-continents, and Scythe-style end-game scoring, without
pre-committing to dynamic membership or overlapping regions.

## Goals

- A `RegionMap` value type that names the partition of a board into
  cell groups.
- An `Influence` function that, given the live `tabletop.State`, a
  `Board`, and a user-supplied owner attribution, returns
  per-region per-player counts.
- A `Score` function that maps influence to points under three scoring
  vocabularies: plurality, top-N (configurable per-place points),
  threshold-payout.
- A tie-break vocabulary covering the three rules every area-control
  game uses: split-the-pot, no-award-on-tie, both-award-on-tie, plus a
  custom callback escape hatch.
- A `CountByCCGOwner` convenience that reads
  `ccg.Entity.Attrs["owner"]` so games that pair `ccg` + `tabletop`
  (the common shape) get influence computation for free.
- JSON-persistable in the existing module style.
- MCP/Starlark bridge ops that mirror the in-Go API.

## Non-goals

- **Dynamic membership.** Regions are fixed at setup; cells do not
  join or leave during play. Games where the map itself changes
  (Tigris & Euphrates, some 4X-style scoring) are out of scope; if a
  driving game appears, this becomes a follow-up extension.
- **Overlapping regions.** Each cell belongs to at most one region.
  Multi-zone scoring (e.g. a hex that scores for both a kingdom and a
  river) is out of scope. Workaround: define two parallel
  `RegionMap`s on the same board.
- **Built-in scoring history.** Cross-round scoring (El Grande's three
  rounds, Blood Rage's three Ages) is game-state, not module-state.
  The module returns "points awarded by this scoring call" — the game
  accumulates them.
- **Region ownership tracking.** "Player X controls region Y" is a
  game-state question with its own thresholds and tie-rules — not
  every game even has the concept (El Grande awards points; control
  is incidental). Influence is the primitive; ownership is the
  derived rule.
- **Pathfinding / contiguity checks.** Regions are sets of cells; we
  don't ask whether they're contiguous, whether a player's units in a
  region are connected, etc. Use `Board.Neighbors` if you need this.

## Packaging

Two options, decision deferred to the implementation plan:

1. **In `modules/tabletop` directly** as `tabletop.RegionMap`. Cleanest
   composition with `tabletop.State`. Slight bloat to the tabletop API
   surface but conceptually tight.
2. **As `modules/tabletop/regions`** sub-package. Cleaner separation;
   tabletop-without-regions stays slim; the sub-package imports
   tabletop and adds the partition layer. Mirrors `modules/ccg/typed`.

Recommendation: option 2 (sub-package). It matches the existing
`ccg/typed` pattern and lets games that don't need regions skip the
extra surface. The spec is written assuming option 2.

## Types

```go
package regions

import "github.com/tjcran/boardgame-go/modules/tabletop"

// RegionID is the stable, JSON-safe name of a region. Use semantic
// names ("anduin", "minas-tirith") over opaque IDs — designers,
// replays, and MCP traces all read this.
type RegionID string

// Region is one named cell group. Cells must be in-bounds for the
// associated Board, and must not overlap with any other region in
// the same Map. The library does NOT enforce contiguity.
type Region struct {
    ID    RegionID    `json:"id"`
    Cells []tabletop.Pos `json:"cells"`
    Label string      `json:"label,omitempty"` // human-readable; optional
}

// Map is the partition of a board into named regions. Build once at
// Setup time (or on first move via mc.Random, if cell assignment is
// random) and store on G. Map itself is JSON-safe.
type Map struct {
    Regions []Region `json:"regions"`

    // cellToRegion is a lazy reverse index; not serialised.
    cellToRegion map[tabletop.Pos]RegionID
}

// NewMap validates the partition (no overlaps, no duplicate IDs) and
// returns a ready Map. Returns an error if any cell appears in more
// than one region or any RegionID is empty / duplicate.
func NewMap(regions []Region) (*Map, error)

// Of returns the region containing pos. The boolean is false when
// pos belongs to no region (not all cells need to be covered — the
// "wasteland" between provinces is fine).
func (m *Map) Of(pos tabletop.Pos) (RegionID, bool)

// Cells returns the cells of the named region, or nil if no such
// region exists. The returned slice is a defensive copy.
func (m *Map) Cells(id RegionID) []tabletop.Pos
```

## Influence computation

```go
// OwnerFn maps a tabletop.UnitID to the playerID that owns it.
// Returning "" means "no owner" — the unit is counted in no player's
// influence but still occupies its cell.
type OwnerFn func(tabletop.UnitID) string

// Influence walks every region's cells, asks the tabletop.State
// who is sitting in each cell, attributes each unit to its owner
// via owner, and tallies. The result is per-region, per-player count.
//
// Players who hold 0 influence in a region simply have no entry —
// callers iterating the inner map should treat absence as zero.
func (m *Map) Influence(s *tabletop.State, owner OwnerFn) map[RegionID]map[string]int
```

### `CountByCCGOwner` convenience

For games that already use `ccg` + `tabletop` (the canonical pairing —
units are entities, ownership lives on `Entity.Attrs["owner"]`):

```go
package regions

import "github.com/tjcran/boardgame-go/modules/ccg"

// ByCCGOwner is an OwnerFn that reads each unit's owner attribute
// from a ccg.State. The unitID is reinterpreted as a ccg.EntityID
// (they share uint64 representation). Unknown entities and entities
// with empty owner return "".
func ByCCGOwner(cs *ccg.State) OwnerFn {
    return func(u tabletop.UnitID) string {
        e, ok := cs.Entities[ccg.EntityID(u)]
        if !ok { return "" }
        return e.AttrStr("owner", "")
    }
}
```

The reinterpretation of `UnitID ↔ EntityID` deserves a note in
package docs: games that pair `ccg` and `tabletop` already do this
implicitly (an entity is placed on the board). This helper formalises
it.

## Scoring

```go
// ScoringKind enumerates the three rule shapes the standard scope
// supports.
type ScoringKind int

const (
    Plurality ScoringKind = iota // single winner per region
    TopN                         // 1st, 2nd, ... get configurable points
    Threshold                    // every player above Threshold gets Payout
)

// TieBreak controls what happens when two or more players tie for
// the same place under any scoring rule.
type TieBreak int

const (
    // Split divides the pot equally and rounds down. If 2 players
    // tie for 1st place under Plurality (1pt), each gets 0. Under
    // TopN [4,2,0] tied for 1st, each gets (4+2)/2 = 3 and 3rd-place
    // skips to position 3.
    Split TieBreak = iota

    // NoAward gives nothing to anyone on a tie for that place.
    NoAward

    // BothAward gives each tied player the full place's points;
    // subsequent places still receive their points (the pot grows).
    BothAward

    // Custom delegates to ScoringRule.CustomTie.
    Custom
)

// CustomTieFn returns each player's awarded points for one region,
// given the per-player influence map and the rule itself. Called
// only when TieBreak == Custom.
type CustomTieFn func(infl map[string]int, rule ScoringRule) map[string]int

// ScoringRule packages the rule for one Score call.
type ScoringRule struct {
    Kind      ScoringKind

    // PerPlace points; index 0 is 1st place, index 1 is 2nd, etc.
    // Required for TopN. Players ranked beyond len(PerPlace) score 0.
    // For Plurality, set PerPlace = []int{points} (single place).
    PerPlace  []int

    // Threshold rule: minimum influence required to qualify.
    Threshold int

    // Threshold rule: points awarded to each qualifying player.
    Payout    int

    // TieBreak rule applied at every place where players tie.
    TieBreak  TieBreak

    // CustomTie is consulted only when TieBreak == Custom.
    CustomTie CustomTieFn
}

// ScoreRegion awards points for a single region's influence map.
// Returns playerID → points awarded by this call.
func ScoreRegion(infl map[string]int, rule ScoringRule) map[string]int

// ScoreAll iterates every region in m and sums per-player points
// across all regions. Equivalent to calling ScoreRegion in a loop
// and summing.
func (m *Map) ScoreAll(
    s *tabletop.State,
    owner OwnerFn,
    rule ScoringRule,
) map[string]int
```

### Worked example: El Grande regional scoring

```go
rule := regions.ScoringRule{
    Kind:     regions.TopN,
    PerPlace: []int{4, 2, 0}, // 1st: 4pts, 2nd: 2pts, 3rd: 0
    TieBreak: regions.Split,
}
pts := regionMap.ScoreAll(g.Space, regions.ByCCGOwner(g.State), rule)
for pid, p := range pts {
    g.Scores[pid] += p
}
```

### Worked example: Risk-style continent bonus

Risk needs *two* scoring passes per round: per-territory plurality
gives 1pt each, and full-continent control (which is a separate
RegionMap layered over the same cells) gives bonus armies.

```go
territoryPts := territoryMap.ScoreAll(g.Space, owner, regions.ScoringRule{
    Kind:     regions.Plurality,
    PerPlace: []int{1},
    TieBreak: regions.NoAward, // contested = no control
})
continentPts := continentMap.ScoreAll(g.Space, owner, regions.ScoringRule{
    Kind:      regions.Threshold,
    Threshold: continentSize, // every cell must be yours
    Payout:    continentBonus,
})
```

Both maps coexist; the second layers higher-level groupings on top.
No primitive bypass — the game just composes two `RegionMap`s.

## Integration with `tabletop.State`

The `Map` does **not** embed or own `tabletop.State`. It is a pure
read-side overlay: build once, store on `G`, query against the live
state every time you need influence. This keeps two important
properties:

- **Single source of truth.** Unit positions live in
  `tabletop.State`. `Map` never duplicates them.
- **No mutation surface to forget.** A game adding new units calls
  `state.Place(unit, pos)` as it always did; influence updates
  automatically next time `Map.Influence` runs.

`Map`'s only mutable contract is the lazy reverse-index
(`cellToRegion`). Like `tabletop.byCell`, it's nilable and built on
first query.

## JSON persistence

Per the existing module persistence pattern (see PR #73 / the
module-state-persistence plan), each module registers a
`Rehydrate(name, rawMap) → (any, error)` function. For the regions
sub-package:

```go
func Rehydrate(_ string, raw map[string]any) (any, error) {
    // Decode raw["regions"] into []Region; lazily rebuild cellToRegion
    // on first Of() call.
}
```

`Map.Regions` is a deterministically-ordered slice (insertion order
preserved) for stable replay digests. `Region.Cells` keeps the
order callers supplied at `NewMap` time; `Of` does not depend on it.

A `MarshalJSON` is unnecessary — default Go JSON over the exported
fields produces the right wire format, mirroring how `tabletop.State`
serialises `Positions` only and rebuilds `byCell` lazily.

## MCP / Starlark bridge

Mirroring `mcp/modulebridge/tabletop.go` (12 ops, see
exploration notes), the regions sub-package adds 5 ops:

| Op | Tool name | Purpose |
|---|---|---|
| `new_map` | `regions_new_map` | Build a `Map` from a list of `{id, cells}` records |
| `of` | `regions_of` | Look up the region containing a position |
| `cells` | `regions_cells` | List the cells of a named region |
| `influence` | `regions_influence` | Compute per-region per-player influence; takes an `owner_attr` string defaulting to `"owner"`, which is read off ccg entities |
| `score_all` | `regions_score_all` | One-shot ScoreAll over a serialised `ScoringRule` |

The bridge uses the same `jsonRehydrate("regions", m)` shape that
tabletop uses. `ScoringRule` over the wire is the same struct with
`kind` as the string discriminator (`"plurality"|"top_n"|
"threshold"`).

## Validation strategy

Two toy reference games under `games/`, both small enough to live
inline with the module rather than as cookbook artifacts:

1. **`games/threeprovinces`** — 4×4 square board partitioned into
   three regions. Two players alternate placing 1 unit per turn for
   4 turns each (8 total moves). End-of-game `OnEnd` calls
   `ScoreAll` with `Plurality` + `Split`. Tests Map construction,
   influence computation, plurality scoring, and the standard tie
   rules. Target: ~150 LOC.

2. **`games/risklike`** — 6 territories grouped into 2 continents
   (a `RegionMap` layered over a `RegionMap`). After a fixed
   placement phase, awards 1pt per controlled territory + a 3pt
   bonus per fully-controlled continent. Tests:
   - Two coexisting `RegionMap`s on the same board.
   - `Threshold` scoring with the threshold = cells-per-continent.
   - `NoAward` tie rule (contested continents score nothing).
   Target: ~200 LOC.

These are validation games, not gameplay-ready ones; both have
fixed pre-scripted setup data so tests can deterministically assert
the scoring math.

If either game's implementation reaches for a primitive not in this
spec, the gap is real — and the spec gets revised before
implementation lands.

## Critical files (implementation will touch)

- `modules/tabletop/regions/regions.go` — `Map`, `Region`, `NewMap`,
  `Of`, `Cells`, `Influence`, `OwnerFn`, `ByCCGOwner`.
- `modules/tabletop/regions/scoring.go` — `ScoringKind`, `TieBreak`,
  `ScoringRule`, `CustomTieFn`, `ScoreRegion`, `ScoreAll`.
- `modules/tabletop/regions/regions_test.go` — unit tests for
  partition validation, influence calculation, every scoring rule
  × every tie-break combination.
- `mcp/modulebridge/regions.go` — five MCP ops listed above + the
  Rehydrate registration.
- `games/threeprovinces/` and `games/risklike/` — validation games.

No changes to existing `tabletop/*` files are required. No changes
to `core/*` are required.

## Verification

The spec holds if:

1. Both validation games compile against the new module surface with
   no fields or methods absent.
2. `threeprovinces` scores correctly under all four tie-rules (Split,
   NoAward, BothAward — and a Custom callback that picks the
   alphabetically-first playerID, just to exercise the path).
3. `risklike` correctly identifies controlled continents under
   `Threshold` and skips contested ones under `NoAward`.
4. JSON round-trip of a `Map` produces a byte-identical structure
   after `NewMap` reconstruction (modulo the lazy reverse index).
5. The MCP bridge ops appear in `mcp_tools_list` and round-trip
   one full influence + scoring sequence via the bridge.

## Open questions for review

- **Packaging:** sub-package vs in `tabletop/`. Recommended:
  sub-package (`modules/tabletop/regions`).
- **`UnitID ↔ EntityID` reinterpretation:** explicit cast in
  `ByCCGOwner` is fine, but worth documenting prominently in the
  package overview that the canonical pairing assumes this.
- **Split rounding:** `Split` truncates per-place pots; integer
  division loses points on odd splits (e.g., two-way tie for a
  3-point place gives each 1, dropping 1pt). Acceptable for area
  control, but explicit in docs.
- **Threshold + ties:** when N players all hit the threshold, the
  standard rule is "all qualify; each gets `Payout`". No tie-break
  needed — `Threshold` is by construction not a single-winner rule.
  Confirmed in scope.
