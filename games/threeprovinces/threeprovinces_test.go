package threeprovinces

import (
	"testing"

	"github.com/tjcran/boardgame-go/core"
)

// TestPluralityScoringEndOfGame plays 8 moves (4 per player). The
// scripted plan is engineered so alice cleanly wins top (2 vs 1) and
// ties bob in both middle (1 vs 1) and bottom (2 vs 2). With Plurality
// + Split: alice gets 1 from top; middle and bottom tie at 1pt and
// split (1/2 = 0 each). Final: alice 1, bob 0.
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
		{0, 0, 2}, // alice bottom (1 vs 1)
		{1, 0, 3}, // bob bottom (1 vs 2)
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
	if scores["1"] != 0 {
		t.Errorf("bob score = %v, want 0", scores["1"])
	}
}
