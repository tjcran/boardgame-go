package risklike

import (
	"testing"

	"github.com/tjcran/boardgame-go/core"
)

// TestContinentBonusAwardedOnlyOnSweep places units such that alice
// sweeps the north continent (all 3 territories) and grabs one south
// territory, while bob occupies one south territory (and two far-away
// throwaway cells used to satisfy the 8-move requirement). Continent
// scoring uses NoAward on ties (irrelevant here since neither player
// sweeps south), but the threshold of 3 ensures only fully-controlled
// continents score the bonus.
//
// Expected:
//
//	alice: 4 territories (3 north + 1 south) × 1 = 4 territory pts +
//	       3 continent bonus (swept north) = 7.
//	bob:   1 south territory × 1 = 1 (bob's two far cells aren't in
//	       either RegionMap). No continent bonus.
func TestContinentBonusAwardedOnlyOnSweep(t *testing.T) {
	g := New()
	s := core.NewMatch(g, 2, nil)

	plan := [][3]int{
		{0, 0, 0}, // alice north_west
		{1, 5, 5}, // bob throwaway (off-map for regions)
		{0, 1, 0}, // alice north_mid
		{1, 5, 6}, // bob throwaway
		{0, 2, 0}, // alice north_east (sweeps north)
		{1, 5, 7}, // bob throwaway
		{0, 0, 1}, // alice south_west
		{1, 1, 1}, // bob south_mid (contested continent)
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
	scores := s.Ctx.Gameover.(map[string]any)

	if scores["0"] != 7 {
		t.Errorf("alice score = %v, want 7 (4 territories + 3 continent bonus)", scores["0"])
	}
	if scores["1"] != 1 {
		t.Errorf("bob score = %v, want 1 (1 south territory, no continent)", scores["1"])
	}
}
