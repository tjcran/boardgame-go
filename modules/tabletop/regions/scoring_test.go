package regions

import "testing"

func TestPluralityClearWinner(t *testing.T) {
	infl := map[string]int{"alice": 3, "bob": 1}
	rule := ScoringRule{Kind: Plurality, PerPlace: []int{5}, TieBreak: Split}
	got := ScoreRegion(infl, rule)
	if got["alice"] != 5 || got["bob"] != 0 {
		t.Fatalf("got %v, want alice:5 bob:0", got)
	}
}

func TestPluralityTieSplit(t *testing.T) {
	infl := map[string]int{"alice": 2, "bob": 2}
	rule := ScoringRule{Kind: Plurality, PerPlace: []int{5}, TieBreak: Split}
	got := ScoreRegion(infl, rule)
	// 5 / 2 = 2 per tied player (truncated)
	if got["alice"] != 2 || got["bob"] != 2 {
		t.Fatalf("got %v, want alice:2 bob:2 (split rounds down)", got)
	}
}

func TestPluralityTieNoAward(t *testing.T) {
	infl := map[string]int{"alice": 2, "bob": 2}
	rule := ScoringRule{Kind: Plurality, PerPlace: []int{5}, TieBreak: NoAward}
	got := ScoreRegion(infl, rule)
	if got["alice"] != 0 || got["bob"] != 0 {
		t.Fatalf("got %v, want zero for both", got)
	}
}

func TestPluralityTieBothAward(t *testing.T) {
	infl := map[string]int{"alice": 2, "bob": 2}
	rule := ScoringRule{Kind: Plurality, PerPlace: []int{5}, TieBreak: BothAward}
	got := ScoreRegion(infl, rule)
	if got["alice"] != 5 || got["bob"] != 5 {
		t.Fatalf("got %v, want alice:5 bob:5 (BothAward)", got)
	}
}

func TestPluralityEmpty(t *testing.T) {
	got := ScoreRegion(map[string]int{}, ScoringRule{
		Kind: Plurality, PerPlace: []int{5}, TieBreak: Split,
	})
	if len(got) != 0 {
		t.Fatalf("empty influence should produce empty result, got %v", got)
	}
}
