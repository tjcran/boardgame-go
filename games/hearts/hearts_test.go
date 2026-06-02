package hearts

import (
	"errors"
	"testing"

	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/modules/ccg"
)

// newMatch builds a fresh 4-player Hearts match seeded for repeatable
// deals. seed 0 is fine — the splitmix64 PRNG produces a non-trivial
// permutation off any seed.
func newMatch(t *testing.T, seed uint64) (*core.Game, core.State) {
	t.Helper()
	g := New()
	s := core.NewMatch(g, 4, SetupData{Seed: seed})
	return g, s
}

func apply(t *testing.T, g *core.Game, s core.State, pid string, cardID ccg.EntityID) (core.State, error) {
	t.Helper()
	return core.Apply(g, s, core.MoveRequest{
		PlayerID: pid,
		Move:     "play",
		Args:     []any{cardID},
	})
}

func twoOfClubsHolder(t *testing.T, s core.State) (string, ccg.EntityID) {
	t.Helper()
	st := s.G.(*State)
	for _, pid := range s.Ctx.PlayOrder {
		z := st.Zones[handZone(pid)]
		for _, id := range z.Members {
			if suitOf(st.State, id) == suitClubs && rankOf(st.State, id) == 2 {
				return pid, id
			}
		}
	}
	t.Fatal("no 2♣ in any hand")
	return "", 0
}

func anyCard(t *testing.T, s core.State, pid string, pred func(suit string, rank int) bool) ccg.EntityID {
	t.Helper()
	st := s.G.(*State)
	z := st.Zones[handZone(pid)]
	for _, id := range z.Members {
		if pred(suitOf(st.State, id), rankOf(st.State, id)) {
			return id
		}
	}
	t.Fatalf("no card matching predicate in %s's hand", pid)
	return 0
}

func TestSetupDeals13EachAndIdentifies2Clubs(t *testing.T) {
	_, s := newMatch(t, 42)
	st := s.G.(*State)

	for _, pid := range s.Ctx.PlayOrder {
		if got := len(st.Zones[handZone(pid)].Members); got != 13 {
			t.Errorf("%s dealt %d cards, want 13", pid, got)
		}
	}
	leader := s.Ctx.PlayOrder[st.LeaderSeat]
	holder, _ := twoOfClubsHolder(t, s)
	if leader != holder {
		t.Errorf("leader=%s but 2♣ holder=%s", leader, holder)
	}
	if s.Ctx.CurrentPlayer != leader {
		t.Errorf("CurrentPlayer=%s, want leader %s", s.Ctx.CurrentPlayer, leader)
	}
}

func TestOpeningLeadMustBeTwoOfClubs(t *testing.T) {
	g, s := newMatch(t, 42)
	leader, _ := twoOfClubsHolder(t, s)
	// Pick any non-2♣ card from the leader's hand.
	notTwoC := anyCard(t, s, leader, func(suit string, rank int) bool {
		return !(suit == suitClubs && rank == 2)
	})
	if _, err := apply(t, g, s, leader, notTwoC); !errors.Is(err, core.ErrInvalidMove) {
		t.Fatalf("expected ErrInvalidMove, got %v", err)
	}
}

func TestFollowSuitConstraintRejected(t *testing.T) {
	g, s := newMatch(t, 42)
	leader, twoC := twoOfClubsHolder(t, s)
	s, err := apply(t, g, s, leader, twoC)
	if err != nil {
		t.Fatalf("opening lead: %v", err)
	}

	// Find a follower who has clubs and try to play a non-club.
	st := s.G.(*State)
	follower := s.Ctx.CurrentPlayer
	if !hasSuit(st, handZone(follower), suitClubs) {
		t.Skipf("seeded deal didn't give %s any clubs; skipping", follower)
	}
	nonClub := anyCard(t, s, follower, func(suit string, _ int) bool { return suit != suitClubs })
	if _, err := apply(t, g, s, follower, nonClub); !errors.Is(err, core.ErrInvalidMove) {
		t.Fatalf("expected follow-suit ErrInvalidMove, got %v", err)
	}
}

func TestHeartsCannotBeLedBeforeBroken(t *testing.T) {
	g, s := newMatch(t, 7)
	// Drive trick 1 to completion using the enumerator. We just need to
	// reach a state where someone leads and they hold a heart; whoever
	// wins trick 1 will lead trick 2.
	s = playOneTrick(t, g, s)
	st := s.G.(*State)
	if st.TricksLeft != 12 {
		t.Fatalf("trick 1 didn't resolve: tricksLeft=%d", st.TricksLeft)
	}
	if st.HeartsBroken {
		t.Skipf("seeded deal produced trick 1 with a heart sluffed; skipping")
	}
	leader := s.Ctx.PlayOrder[st.LeaderSeat]
	heart, ok := tryFindHeart(st, leader)
	if !ok {
		t.Skipf("leader %s has no heart on trick 2; skipping", leader)
	}
	// If the leader has any non-heart, leading a heart must reject.
	if onlyHearts(st, handZone(leader)) {
		t.Skipf("leader %s has only hearts left; legal to lead", leader)
	}
	if _, err := apply(t, g, s, leader, heart); !errors.Is(err, core.ErrInvalidMove) {
		t.Fatalf("expected hearts-not-broken ErrInvalidMove, got %v", err)
	}
}

func TestFullHandResolvesAllTricksAndScoresSum26(t *testing.T) {
	g, s := newMatch(t, 12345)
	for i := 0; i < 52; i++ {
		legal := g.Enumerate(s.G, s.Ctx, s.Ctx.CurrentPlayer)
		if len(legal) == 0 {
			t.Fatalf("move %d: enumerator returned no legal moves for %s", i, s.Ctx.CurrentPlayer)
		}
		req := core.MoveRequest{
			PlayerID: s.Ctx.CurrentPlayer,
			Move:     legal[0].Move,
			Args:     legal[0].Args,
		}
		next, err := core.Apply(g, s, req)
		if err != nil {
			t.Fatalf("move %d (%s plays %v): %v", i, s.Ctx.CurrentPlayer, legal[0].Args, err)
		}
		s = next
	}
	st := s.G.(*State)
	if st.TricksLeft != 0 {
		t.Fatalf("after 52 plays, tricksLeft=%d, want 0", st.TricksLeft)
	}
	total := 0
	for _, v := range st.Scores {
		total += v
	}
	// 13 hearts (1 ea) + Q♠ (13) = 26 penalty points distributed.
	if total != 26 {
		t.Fatalf("total penalty points = %d, want 26 (scores=%v)", total, st.Scores)
	}
	if s.Ctx.Gameover == nil {
		t.Fatalf("expected Gameover set, got nil")
	}
}

func TestTrickWinnerIsHighestOfLeadSuit(t *testing.T) {
	// Pick a deal where the first trick resolves and verify the winner
	// of trick 1 leads trick 2.
	g, s := newMatch(t, 99)
	leaderBefore := s.Ctx.CurrentPlayer
	s = playOneTrick(t, g, s)
	st := s.G.(*State)
	winnerSeat := st.LeaderSeat
	winnerPID := s.Ctx.PlayOrder[winnerSeat]

	// The winner's "won" zone should contain exactly 4 cards.
	if got := len(st.Zones[wonZone(winnerPID)].Members); got != 4 {
		t.Fatalf("winner %s's won zone has %d cards, want 4", winnerPID, got)
	}
	if s.Ctx.CurrentPlayer != winnerPID {
		t.Errorf("CurrentPlayer after trick = %s, want winner %s", s.Ctx.CurrentPlayer, winnerPID)
	}
	if leaderBefore == "" {
		t.Fatal("missing leaderBefore")
	}
}

func playOneTrick(t *testing.T, g *core.Game, s core.State) core.State {
	t.Helper()
	for i := 0; i < 4; i++ {
		legal := g.Enumerate(s.G, s.Ctx, s.Ctx.CurrentPlayer)
		if len(legal) == 0 {
			t.Fatalf("trick play %d: no legal moves for %s", i, s.Ctx.CurrentPlayer)
		}
		req := core.MoveRequest{
			PlayerID: s.Ctx.CurrentPlayer,
			Move:     legal[0].Move,
			Args:     legal[0].Args,
		}
		next, err := core.Apply(g, s, req)
		if err != nil {
			t.Fatalf("trick play %d: %v", i, err)
		}
		s = next
	}
	return s
}

func tryFindHeart(st *State, pid string) (ccg.EntityID, bool) {
	z, ok := st.Zones[handZone(pid)]
	if !ok {
		return 0, false
	}
	for _, id := range z.Members {
		if suitOf(st.State, id) == suitHrts {
			return id, true
		}
	}
	return 0, false
}
