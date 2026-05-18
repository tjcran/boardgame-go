// Package tabletop provides spatial + dice primitives for miniatures
// wargames (40k-shaped, Battletech-shaped, Necromunda-shaped) built on
// boardgame-go.
//
// Concerns:
//   - Grid geometry (square / hex) via a Board interface
//   - Per-unit positions with reverse cell→units index
//   - Terrain tags (cover, blocks LOS, difficult, …)
//   - LOS as a Board-agnostic free function with a blocker predicate
//   - Dice pools and the hit→wound→save resolution chain
//
// What this package does NOT do:
//   - Define what a "unit" is — that's the game's job. tabletop tracks
//     positions for opaque uint64 UnitIDs and nothing else. Pair with
//     ccg.State for stat blocks / modifiers / events, or roll your own.
//   - Pick a measurement system. Distance is in cells, not inches.
//     Continuous-2D / measured-tabletop is out of scope for v1; the
//     Board interface is the extension point.
//   - Manage activation order — that's core.Turn / core.Phase.
//
// The library is optional. Importing tabletop costs you nothing if your
// game isn't spatial; the engine never imports it. Spatial games drop a
// *tabletop.State alongside their other state in G.
//
// Minimal sketch (composing with ccg):
//
//	type MyG struct {
//	    *ccg.State                   // entities, stats, modifiers, events
//	    Space *tabletop.State        // positions
//	    Board tabletop.Board         // geometry
//	    Terrain *tabletop.TerrainMap // cover & LOS blocking
//	}
//
//	func setupGame(_ core.Ctx, _ any) core.G {
//	    g := &MyG{
//	        State:   ccg.NewState(),
//	        Space:   tabletop.NewState(),
//	        Board:   tabletop.NewSquareBoard(24, 36),
//	        Terrain: tabletop.NewTerrainMap(),
//	    }
//	    unit := g.NewEntity("marine", "0", map[string]any{"hp": 2})
//	    g.Space.Place(tabletop.UnitID(unit), tabletop.Pos{X: 5, Y: 5})
//	    return g
//	}
package tabletop
