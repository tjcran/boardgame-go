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

func TestTopNNoTies(t *testing.T) {
	infl := map[string]int{"alice": 5, "bob": 3, "carol": 1, "dave": 0}
	rule := ScoringRule{Kind: TopN, PerPlace: []int{4, 2, 0}, TieBreak: Split}
	got := ScoreRegion(infl, rule)
	want := map[string]int{"alice": 4, "bob": 2, "carol": 0, "dave": 0}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: got %d, want %d", k, got[k], v)
		}
	}
}

func TestTopNTieAtFirstSplit(t *testing.T) {
	infl := map[string]int{"alice": 5, "bob": 5, "carol": 1}
	rule := ScoringRule{Kind: TopN, PerPlace: []int{4, 2, 0}, TieBreak: Split}
	got := ScoreRegion(infl, rule)
	// 1st+2nd pot = 6; split = 3 each. carol gets 3rd-place (0).
	if got["alice"] != 3 || got["bob"] != 3 {
		t.Errorf("alice/bob tie split: got alice=%d bob=%d, want 3/3", got["alice"], got["bob"])
	}
	if got["carol"] != 0 {
		t.Errorf("carol: got %d, want 0 (3rd place)", got["carol"])
	}
}

func TestTopNTieAtFirstBothAward(t *testing.T) {
	infl := map[string]int{"alice": 5, "bob": 5, "carol": 1}
	rule := ScoringRule{Kind: TopN, PerPlace: []int{4, 2, 0}, TieBreak: BothAward}
	got := ScoreRegion(infl, rule)
	// Both get 4; carol drops to 2nd place (carol scores 2).
	if got["alice"] != 4 || got["bob"] != 4 {
		t.Errorf("BothAward tie: got alice=%d bob=%d, want 4/4", got["alice"], got["bob"])
	}
	if got["carol"] != 2 {
		t.Errorf("carol after BothAward tie: got %d, want 2", got["carol"])
	}
}

func TestTopNTieAtFirstNoAward(t *testing.T) {
	infl := map[string]int{"alice": 5, "bob": 5, "carol": 1}
	rule := ScoringRule{Kind: TopN, PerPlace: []int{4, 2, 0}, TieBreak: NoAward}
	got := ScoreRegion(infl, rule)
	// 1st-place tie: nobody scores. carol = 3rd (0).
	if got["alice"] != 0 || got["bob"] != 0 || got["carol"] != 0 {
		t.Errorf("NoAward should zero everyone: got %v", got)
	}
}

func TestTopNFewerPlayersThanPlaces(t *testing.T) {
	infl := map[string]int{"alice": 3}
	rule := ScoringRule{Kind: TopN, PerPlace: []int{4, 2, 0}, TieBreak: Split}
	got := ScoreRegion(infl, rule)
	if got["alice"] != 4 {
		t.Errorf("alice: got %d, want 4", got["alice"])
	}
}
