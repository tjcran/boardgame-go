package match

import (
	"testing"

	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/games/tictactoe"
	"github.com/tjcran/boardgame-go/storage"
)

func TestExportImportRoundTrip(t *testing.T) {
	m := NewManager(storage.NewMemory())
	m.MustRegister(tictactoe.New())

	id, _ := m.Create("tic-tac-toe", CreateOptions{})
	alice, _ := m.Join(id, "alice", JoinOptions{})
	bob, _ := m.Join(id, "bob", JoinOptions{})

	// Play a few moves.
	plays := []struct {
		pid, creds string
		cell       int
	}{
		{alice.PlayerID, alice.PlayerCredentials, 4},
		{bob.PlayerID, bob.PlayerCredentials, 0},
		{alice.PlayerID, alice.PlayerCredentials, 1},
	}
	for _, p := range plays {
		if _, err := m.Move(id, p.pid, p.creds, "clickCell", []any{p.cell}); err != nil {
			t.Fatalf("move: %v", err)
		}
	}

	exp, err := m.ExportMatch(id)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if exp.GameName != "tic-tac-toe" || len(exp.Log) != 3 {
		t.Fatalf("unexpected export: %+v", exp)
	}

	// Fresh manager, fresh storage — only the export.
	m2 := NewManager(storage.NewMemory())
	m2.MustRegister(tictactoe.New())
	newID, err := m2.ImportMatch(exp)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	mm, err := m2.State(newID)
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	// The replayed cells should match the source.
	st := mm.State.G.(*tictactoe.State)
	if st.Cells[4] != "X" || st.Cells[0] != "O" || st.Cells[1] != "X" {
		t.Fatalf("expected replayed cells X@4 O@0 X@1, got %+v", st.Cells)
	}
	// And the current player should match — turn 4, X's turn since
	// MaxMoves=1 auto-ends turns.
	if mm.State.Ctx.Turn != 4 {
		t.Fatalf("expected turn 4 after 3 moves, got %d", mm.State.Ctx.Turn)
	}
	_ = core.MoveRequest{} // silence unused import
}
