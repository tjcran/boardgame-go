package match

import (
	"testing"

	"github.com/tjcran/boardgame-go/games/tictactoe"
	"github.com/tjcran/boardgame-go/storage"
)

func TestBGIO574_FindByJoinCode(t *testing.T) {
	m := NewManager(storage.NewMemory())
	m.MustRegister(tictactoe.New())

	id, err := m.Create("tic-tac-toe", CreateOptions{
		Name:     "Friday Night Catan",
		JoinCode: "ABC123",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := m.FindByJoinCode("tic-tac-toe", "ABC123")
	if err != nil {
		t.Fatalf("FindByJoinCode: %v", err)
	}
	if got.ID != id {
		t.Fatalf("expected id %s, got %s", id, got.ID)
	}
	if got.Name != "Friday Night Catan" {
		t.Fatalf("expected name preserved, got %q", got.Name)
	}
}

func TestBGIO574_DuplicateJoinCodeRejected(t *testing.T) {
	m := NewManager(storage.NewMemory())
	m.MustRegister(tictactoe.New())
	if _, err := m.Create("tic-tac-toe", CreateOptions{JoinCode: "DUPE01"}); err != nil {
		t.Fatalf("first create: %v", err)
	}
	if _, err := m.Create("tic-tac-toe", CreateOptions{JoinCode: "DUPE01"}); err == nil {
		t.Fatal("expected dup-code rejection")
	}
}

func TestBGIO574_FindByJoinCodeUnknownReturnsNotFound(t *testing.T) {
	m := NewManager(storage.NewMemory())
	m.MustRegister(tictactoe.New())
	if _, err := m.FindByJoinCode("tic-tac-toe", "GHOST1"); err != storage.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
