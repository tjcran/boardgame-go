package match

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/tjcran/boardgame-go/games/tictactoe"
	"github.com/tjcran/boardgame-go/storage"
)

// TestBGIO1100_SimultaneousJoinsRaceFree verifies that N goroutines
// joining the same match concurrently each land on a unique seat with
// no infinite-loading behaviour BGIO reported.
//
// Our lockMatch serialises per-match writes, so each Join sees the
// current Players slice and the first-free-seat loop is deterministic.
func TestBGIO1100_SimultaneousJoinsRaceFree(t *testing.T) {
	m := NewManager(storage.NewMemory())
	g := tictactoe.New()
	g.MinPlayers, g.MaxPlayers = 2, 8 // expand cap so 8 racers all fit
	g.AllowDynamicPlayers = true
	m.MustRegister(g)
	id, err := m.Create("tic-tac-toe", CreateOptions{NumPlayers: 2})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	const racers = 8
	var (
		wg     sync.WaitGroup
		seatCh = make(chan string, racers)
		errs   atomic.Int64
	)
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r, err := m.Join(id, "racer", JoinOptions{})
			if err != nil {
				errs.Add(1)
				return
			}
			seatCh <- r.Seat
		}(i)
	}
	wg.Wait()
	close(seatCh)

	if errs.Load() != 0 {
		t.Fatalf("expected 0 join errors, got %d", errs.Load())
	}
	seen := map[string]int{}
	for s := range seatCh {
		seen[s]++
	}
	for s, n := range seen {
		if n != 1 {
			t.Fatalf("seat %s assigned %d times — concurrent joins collided", s, n)
		}
	}
}

// TestBGIO877_UpdatePlayerBroadcastsMatchData reproduces BGIO's bug
// where renaming a player via the Lobby API didn't propagate to
// connected clients. Our UpdatePlayer calls broadcastMatchData
// explicitly.
func TestBGIO877_UpdatePlayerBroadcastsMatchData(t *testing.T) {
	m := NewManager(storage.NewMemory())
	m.MustRegister(tictactoe.New())
	id, _ := m.Create("tic-tac-toe", CreateOptions{})
	alice, _ := m.Join(id, "alice", JoinOptions{})

	sub := &captureSub{}
	unsub := m.Subscribe(id, sub)
	defer unsub()

	newName := "ALICE"
	if err := m.UpdatePlayer(id, alice.PlayerID, alice.PlayerCredentials, UpdatePlayerOpts{
		NewName: &newName,
	}); err != nil {
		t.Fatalf("UpdatePlayer: %v", err)
	}
	if len(sub.matchDatas) == 0 {
		t.Fatal("expected matchData broadcast after UpdatePlayer")
	}
	last := sub.matchDatas[len(sub.matchDatas)-1]
	if last[0].Name != "ALICE" {
		t.Fatalf("expected renamed player in broadcast, got %+v", last)
	}
}
