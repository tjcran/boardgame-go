// Package regions extends modules/tabletop with named cell groups and
// per-region influence scoring — the primitive missing for area-control
// games (El Grande, Blood Rage, Inis, Risk, Scythe).
//
// A Map is a static partition of a Board into named Regions. Each cell
// belongs to at most one region; not every cell needs to belong to one
// (the "wasteland" between provinces is fine).
//
// Influence is computed on demand from the live tabletop.State plus a
// user-supplied OwnerFn that maps a UnitID to a playerID. Games that
// pair ccg + tabletop can use the ByCCGOwner convenience, which reads
// ccg.Entity.Attrs["owner"].
//
// UnitID and EntityID share the underlying uint64 representation. The
// canonical pairing — a tabletop unit IS a ccg entity placed on the
// board — assumes this; ByCCGOwner formalises the reinterpretation.
//
// Scoring rules: Plurality (single winner per region), TopN
// (configurable points per place), Threshold (above-threshold gets
// payout). Tie-breaks: Split, NoAward, BothAward, Custom.
//
// Out of scope (deferred until a driving game asks): dynamic
// membership, overlapping regions, scoring history.
package regions
