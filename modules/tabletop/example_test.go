package tabletop_test

import (
	"testing"

	"github.com/tjcran/boardgame-go/modules/ccg"
	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/plugins/random"
	"github.com/tjcran/boardgame-go/modules/tabletop"
)

// miniWargame demonstrates tabletop + ccg + core composing for a
// 40k-shaped MVP. Two players, two units each, one square board, one
// cover cell, one Shoot move that runs a Resolve and decrements hp.
type wargameG struct {
	*ccg.State                   // units, hp via Attrs, modifiers, events
	Space   *tabletop.State      // positions
	Board   tabletop.Board       // 10×10 square
	Terrain *tabletop.TerrainMap // cover & LOS blockers
}

func wargameGame() *core.Game {
	return &core.Game{
		Name:       "wargame-mini",
		MinPlayers: 2,
		MaxPlayers: 2,
		Plugins:    []core.Plugin{random.New("wargame-example")},
		Setup: func(_ core.Ctx, _ any) core.G {
			g := &wargameG{
				State:   ccg.NewState(),
				Space:   tabletop.NewState(),
				Board:   tabletop.NewSquareBoard(10, 10),
				Terrain: tabletop.NewTerrainMap(),
			}
			// Player 0 unit at (1,1), Player 1 unit at (8,8). One LOS
			// blocker at (5,5) — sits on the diagonal between them.
			a := g.NewEntity("marine", "0", map[string]any{"hp": 5})
			b := g.NewEntity("ork", "1", map[string]any{"hp": 5})
			g.Space.Place(tabletop.UnitID(a), tabletop.Pos{X: 1, Y: 1})
			g.Space.Place(tabletop.UnitID(b), tabletop.Pos{X: 8, Y: 8})
			g.Terrain.Tag(tabletop.Pos{X: 5, Y: 5}, tabletop.TerrainTagBlocksLOS)
			return g
		},
		Moves: map[string]any{
			"shoot": core.MoveFn(func(mc *core.MoveContext, args ...any) (core.G, error) {
				g := mc.G.(*wargameG)
				attackerID := args[0].(ccg.EntityID)
				targetID := args[1].(ccg.EntityID)

				attackerPos, _ := g.Space.PositionOf(tabletop.UnitID(attackerID))
				targetPos, _ := g.Space.PositionOf(tabletop.UnitID(targetID))

				// LOS check — game-side, uses the generic free function.
				if !tabletop.LineOfSight(g.Board, attackerPos, targetPos, g.Terrain.Blocks) {
					return g, nil // no shot
				}
				// Range check.
				if g.Board.Distance(attackerPos, targetPos) > 24 {
					return g, nil
				}

				result := tabletop.Resolve{
					Attacks: 4, HitOn: 3, WoundOn: 4, SaveOn: 4,
				}.Run(mc.Random)

				if result.Unsaved > 0 {
					target, _ := g.Get(targetID)
					hp := target.AttrInt("hp", 0) - result.Unsaved
					if hp < 0 {
						hp = 0
					}
					g.SetAttr(targetID, "hp", hp)
					g.Publish(ccg.Event{
						Type: "damage_dealt", Source: attackerID, Target: targetID,
						Data: map[string]any{"amount": result.Unsaved, "hp_after": hp},
					})
				}
				return g, nil
			}),
		},
		Turn: &core.TurnConfig{MinMoves: 1, MaxMoves: 1},
	}
}

// TestWargameComposition validates the end-to-end story:
//  1. tabletop.State holds positions; ccg.State holds stats
//  2. tabletop.LineOfSight + TerrainMap.Blocks compose without coupling
//  3. Resolve runs through the engine PRNG (deterministic replay)
//  4. ccg.Publish records the damage event
func TestWargameComposition(t *testing.T) {
	g := wargameGame()
	s := core.NewMatch(g, 0, nil)
	wg := s.G.(*wargameG)

	// Find the two units by owner.
	var a, b ccg.EntityID
	for id, e := range wg.Entities {
		if e.Owner == "0" {
			a = id
		} else if e.Owner == "1" {
			b = id
		}
	}
	if a == 0 || b == 0 {
		t.Fatalf("setup should have placed one unit per player; entities=%v", wg.Entities)
	}

	startHP := wg.Entities[b].AttrInt("hp", 0)
	if startHP != 5 {
		t.Fatalf("expected target to start at hp 5, got %d", startHP)
	}

	// First shot: attacker at (1,1), target at (8,8), blocker at (5,5)
	// sits on the diagonal — LOS blocked, no damage.
	// CurrentPlayer starts at "0".
	s, err := core.Apply(g, s, core.MoveRequest{
		PlayerID: "0", Move: "shoot", Args: []any{a, b},
	})
	if err != nil {
		t.Fatalf("shoot (blocked): %v", err)
	}
	wg = s.G.(*wargameG)
	if hp := wg.Entities[b].AttrInt("hp", 0); hp != startHP {
		t.Fatalf("blocker should have prevented damage, hp=%d", hp)
	}

	// Clear the blocker; subsequent shots should be able to land.
	// The turn has now advanced to player "1" (MaxMoves=1).
	wg.Terrain.Untag(tabletop.Pos{X: 5, Y: 5}, tabletop.TerrainTagBlocksLOS)
	eventsBefore := len(wg.Events)

	// Apply several shots until the target dies or we've tried enough.
	// Alternate players each turn — with MaxMoves=1 the engine advances
	// the turn after every move.
	for i := 0; i < 30 && wg.Entities[b].AttrInt("hp", 0) > 0; i++ {
		// Read CurrentPlayer from the state so we never guess wrong.
		playerID := s.Ctx.CurrentPlayer
		s, err = core.Apply(g, s, core.MoveRequest{
			PlayerID: playerID, Move: "shoot", Args: []any{a, b},
		})
		if err != nil {
			t.Fatalf("shoot (clear) iter %d player %s: %v", i, playerID, err)
		}
		wg = s.G.(*wargameG)
	}
	if hp := wg.Entities[b].AttrInt("hp", 0); hp >= startHP {
		t.Fatalf("expected at least one shot to land over 30 tries, hp=%d", hp)
	}
	if len(wg.Events) <= eventsBefore {
		t.Fatalf("expected at least one damage_dealt event published")
	}
}
