package rps

import (
	"errors"
	"testing"

	"github.com/tjcran/boardgame-go/core"
)

func submitReq(player, throw string) core.MoveRequest {
	return core.MoveRequest{PlayerID: player, Move: "submit", Args: []any{throw}}
}

func mustApply(t *testing.T, g *core.Game, s core.State, req core.MoveRequest) core.State {
	t.Helper()
	next, err := core.Apply(g, s, req)
	if err != nil {
		t.Fatalf("apply %s by %s: %v", req.Move, req.PlayerID, err)
	}
	return next
}

func gameState(t *testing.T, s core.State) *State {
	t.Helper()
	st, ok := s.G.(*State)
	if !ok {
		t.Fatalf("G is %T, want *State", s.G)
	}
	return st
}

// A full round resolves atomically: when the last player submits, the same
// Apply call reveals both throws, scores the winner, clears the buffer, and
// re-arms the next round.
func TestRoundResolvesOnLastSubmit(t *testing.T) {
	g := New()
	s := core.NewMatch(g, 2, nil)

	// Round is armed at match start: both players active.
	if got := len(s.Ctx.ActivePlayers); got != 2 {
		t.Fatalf("expected 2 active players at start, got %d", got)
	}

	// Player 0 submits; round not yet resolved.
	s = mustApply(t, g, s, submitReq("0", "rock"))
	if st := gameState(t, s); len(st.LastRound) != 0 {
		t.Fatalf("round resolved early: LastRound=%v", st.LastRound)
	}
	if _, stillActive := s.Ctx.ActivePlayers["0"]; stillActive {
		t.Fatal("player 0 should be removed from active set after submitting")
	}

	// Player 1 submits last → reveal fires in this same Apply.
	s = mustApply(t, g, s, submitReq("1", "scissors"))
	st := gameState(t, s)
	if st.LastRound["0"] != "rock" || st.LastRound["1"] != "scissors" {
		t.Fatalf("expected revealed throws, got LastRound=%v", st.LastRound)
	}
	if len(st.Pending) != 0 {
		t.Fatalf("pending buffer should be cleared after reveal, got %v", st.Pending)
	}
	// rock beats scissors → player 0 scores.
	if st.Scores["0"] != 1 || st.Scores["1"] != 0 {
		t.Fatalf("expected scores 0:1 1:0, got %v", st.Scores)
	}
	if st.Round != 1 {
		t.Fatalf("expected round counter 1, got %d", st.Round)
	}
	// Next round is re-armed.
	if len(s.Ctx.ActivePlayers) != 2 {
		t.Fatalf("expected 2 active players for next round, got %d", len(s.Ctx.ActivePlayers))
	}
}

// A tie scores nobody but still advances the round and re-arms.
func TestTieScoresNobody(t *testing.T) {
	g := New()
	s := core.NewMatch(g, 2, nil)
	s = mustApply(t, g, s, submitReq("0", "paper"))
	s = mustApply(t, g, s, submitReq("1", "paper"))

	st := gameState(t, s)
	if st.Scores["0"] != 0 || st.Scores["1"] != 0 {
		t.Fatalf("tie should score nobody, got %v", st.Scores)
	}
	if st.Round != 1 {
		t.Fatalf("expected round 1 after tie, got %d", st.Round)
	}
}

// Before a player has submitted, no opponent's pending throw is visible to
// them; after both submit, the reveal is public.
func TestPlayerViewHidesPendingThrows(t *testing.T) {
	g := New()
	s := core.NewMatch(g, 2, nil)
	s = mustApply(t, g, s, submitReq("0", "rock"))

	// Player 1's view must not contain player 0's pending throw.
	view1 := core.PlayerView(g, s, "1")
	v1 := gameState(t, view1)
	if _, leaked := v1.Pending["0"]; leaked {
		t.Fatalf("player 0's throw leaked to player 1: %v", v1.Pending)
	}

	// Player 0's own view should still show their own locked-in throw.
	view0 := core.PlayerView(g, s, "0")
	v0 := gameState(t, view0)
	if v0.Pending["0"] != "rock" {
		t.Fatalf("player 0 should see own throw, got %v", v0.Pending)
	}

	// Spectator sees no pending throws.
	viewSpec := core.PlayerView(g, s, "")
	if vs := gameState(t, viewSpec); len(vs.Pending) != 0 {
		t.Fatalf("spectator should see no pending throws, got %v", vs.Pending)
	}
}

// A submit move's args are redacted in the log seen by other seats.
func TestSubmitArgsRedactedForOthers(t *testing.T) {
	g := New()
	s := core.NewMatch(g, 2, nil)
	s = mustApply(t, g, s, submitReq("0", "rock"))

	entry := findMoveLog(t, core.PlayerView(g, s, "1").Log, "submit", "0")
	if len(entry.Args) != 0 {
		t.Fatalf("player 0's submit args should be redacted for player 1, got %v", entry.Args)
	}

	// Player 0 sees their own args un-redacted.
	ownEntry := findMoveLog(t, core.PlayerView(g, s, "0").Log, "submit", "0")
	if len(ownEntry.Args) != 1 || ownEntry.Args[0] != "rock" {
		t.Fatalf("player 0 should see own args, got %v", ownEntry.Args)
	}
}

// A non-active player (already submitted) cannot submit again in the round.
func TestRejectDoubleSubmit(t *testing.T) {
	g := New()
	s := core.NewMatch(g, 2, nil)
	s = mustApply(t, g, s, submitReq("0", "rock"))

	_, err := core.Apply(g, s, submitReq("0", "paper"))
	if !errors.Is(err, core.ErrInactivePlayer) {
		t.Fatalf("expected ErrInactivePlayer for double submit, got %v", err)
	}
}

func TestRejectInvalidThrow(t *testing.T) {
	g := New()
	s := core.NewMatch(g, 2, nil)
	_, err := core.Apply(g, s, submitReq("0", "lizard"))
	if !errors.Is(err, core.ErrInvalidMove) {
		t.Fatalf("expected ErrInvalidMove, got %v", err)
	}
}

// With three players the reveal fires only on the third submission, and
// scoring counts beats against every opponent.
func TestThreePlayerRound(t *testing.T) {
	g := New()
	s := core.NewMatch(g, 3, nil)
	if len(s.Ctx.ActivePlayers) != 3 {
		t.Fatalf("expected 3 active players, got %d", len(s.Ctx.ActivePlayers))
	}

	s = mustApply(t, g, s, submitReq("0", "rock"))
	s = mustApply(t, g, s, submitReq("1", "scissors"))
	// Not resolved until the third player submits.
	if st := gameState(t, s); len(st.LastRound) != 0 {
		t.Fatalf("round resolved before all three submitted: %v", st.LastRound)
	}
	s = mustApply(t, g, s, submitReq("2", "scissors"))

	st := gameState(t, s)
	// rock beats both scissors → player 0 scores 2; the two scissors tie.
	if st.Scores["0"] != 2 || st.Scores["1"] != 0 || st.Scores["2"] != 0 {
		t.Fatalf("expected scores 0:2 1:0 2:0, got %v", st.Scores)
	}
	if len(s.Ctx.ActivePlayers) != 3 {
		t.Fatalf("expected next round re-armed for 3, got %d", len(s.Ctx.ActivePlayers))
	}
}

// Playing rounds until a player reaches the win target ends the match.
func TestMatchEndsAtWinTarget(t *testing.T) {
	g := New()
	s := core.NewMatch(g, 2, nil)
	// Player 0 wins every round (rock vs scissors).
	for i := 0; i < winTarget; i++ {
		if s.Ctx.Gameover != nil {
			t.Fatalf("game ended early after %d rounds", i)
		}
		s = mustApply(t, g, s, submitReq("0", "rock"))
		s = mustApply(t, g, s, submitReq("1", "scissors"))
	}

	over, ok := s.Ctx.Gameover.(map[string]any)
	if !ok {
		t.Fatalf("expected gameover map, got %T %v", s.Ctx.Gameover, s.Ctx.Gameover)
	}
	if over["winner"] != "0" {
		t.Fatalf("expected winner 0, got %v", over)
	}
	if s.Ctx.ActivePlayers != nil {
		t.Fatalf("expected no active players after game over, got %v", s.Ctx.ActivePlayers)
	}
}

// findMoveLog returns the log entry for a move by name and player.
func findMoveLog(t *testing.T, log []core.LogEntry, move, player string) core.LogEntry {
	t.Helper()
	for _, e := range log {
		if e.Move == move && e.PlayerID == player {
			return e
		}
	}
	t.Fatalf("no log entry for move %q by player %q in %+v", move, player, log)
	return core.LogEntry{}
}
