package threeprovinces

import (
	"testing"

	"github.com/tjcran/boardgame-go/core"
)

// TestPluralityScoringEndOfGame plays 8 moves (4 per player). Alice
// wins "top" cleanly (2 vs 1), middle ties 1-1 (Split awards 0 each),
// bob wins "bottom" cleanly (1 vs 2). Expected total: alice 1, bob 1.
// Exercises the Plurality clear-winner path and the Split tie path.
func TestPluralityScoringEndOfGame(t *testing.T) {
	g := New()
	s := core.NewMatch(g, 2, nil)

	plan := [][3]int{
		{0, 0, 0}, // alice top
		{1, 3, 0}, // bob top
		{0, 1, 0}, // alice top (2 vs 1)
		{1, 3, 1}, // bob middle
		{0, 0, 1}, // alice middle (1 vs 1)  -- this ties for middle
		{1, 3, 2}, // bob bottom
		{0, 0, 2}, // alice bottom (now 1 vs 1)
		{1, 0, 3}, // bob bottom (1 vs 2, bob wins)
	}

	for i, mv := range plan {
		pid := "0"
		if mv[0] == 1 {
			pid = "1"
		}
		next, err := core.Apply(g, s, core.MoveRequest{
			PlayerID: pid,
			Move:     "place",
			Args:     []any{mv[1], mv[2]},
		})
		if err != nil {
			t.Fatalf("move %d (%+v): %v", i, mv, err)
		}
		s = next
	}

	if s.Ctx.Gameover == nil {
		t.Fatalf("expected Gameover after 8 placements")
	}
	scores, ok := s.Ctx.Gameover.(map[string]any)
	if !ok {
		t.Fatalf("Gameover not a map: %T", s.Ctx.Gameover)
	}
	if scores["0"] != 1 {
		t.Errorf("alice score = %v, want 1", scores["0"])
	}
	if scores["1"] != 1 {
		t.Errorf("bob score = %v, want 1", scores["1"])
	}
}
