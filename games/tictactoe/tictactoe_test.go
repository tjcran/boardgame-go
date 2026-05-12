package tictactoe

import (
	"errors"
	"testing"

	"github.com/tjcran/boardgame-go/core"
)

// play applies a sequence of (player, cell) moves and returns the final state.
func play(t *testing.T, moves [][2]int) core.State {
	t.Helper()
	g := New()
	s := core.NewMatch(g, 0, nil)
	for i, mv := range moves {
		req := core.MoveRequest{
			PlayerID: itoa(mv[0]),
			Move:     "clickCell",
			Args:     []any{mv[1]},
		}
		next, err := core.Apply(g, s, req)
		if err != nil {
			t.Fatalf("move %d (%+v): %v", i, mv, err)
		}
		s = next
	}
	return s
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	return "1"
}

func gameoverMap(t *testing.T, s core.State) map[string]any {
	t.Helper()
	m, ok := s.Ctx.Gameover.(map[string]any)
	if !ok {
		t.Fatalf("expected map gameover, got %T %+v", s.Ctx.Gameover, s.Ctx.Gameover)
	}
	return m
}

func TestXWinsTopRow(t *testing.T) {
	// X: 0, 1, 2 ; O: 3, 4
	s := play(t, [][2]int{
		{0, 0}, {1, 3}, {0, 1}, {1, 4}, {0, 2},
	})
	go_ := gameoverMap(t, s)
	if go_["winner"] != "0" {
		t.Fatalf("expected winner=0, got %+v", s.Ctx.Gameover)
	}
}

func TestDraw(t *testing.T) {
	// classic draw layout, no 3-in-a-row:
	//   X X O
	//   O O X
	//   X O X
	s := play(t, [][2]int{
		{0, 0}, {1, 2}, {0, 1}, {1, 4}, {0, 5},
		{1, 3}, {0, 6}, {1, 7}, {0, 8},
	})
	go_ := gameoverMap(t, s)
	if go_["draw"] != true {
		t.Fatalf("expected draw=true, got %+v", s.Ctx.Gameover)
	}
}

func TestRejectTakenCell(t *testing.T) {
	g := New()
	s := core.NewMatch(g, 0, nil)
	s, err := core.Apply(g, s, core.MoveRequest{
		PlayerID: "0", Move: "clickCell", Args: []any{0},
	})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	_, err = core.Apply(g, s, core.MoveRequest{
		PlayerID: "1", Move: "clickCell", Args: []any{0},
	})
	if !errors.Is(err, core.ErrInvalidMove) {
		t.Fatalf("expected ErrInvalidMove, got %v", err)
	}
}

func TestRejectOutOfRange(t *testing.T) {
	g := New()
	s := core.NewMatch(g, 0, nil)
	_, err := core.Apply(g, s, core.MoveRequest{
		PlayerID: "0", Move: "clickCell", Args: []any{99},
	})
	if !errors.Is(err, core.ErrInvalidMove) {
		t.Fatalf("expected ErrInvalidMove, got %v", err)
	}
}
