package economy_test

import (
	"testing"

	"github.com/tjcran/boardgame-go/modules/ccg"
	"github.com/tjcran/boardgame-go/modules/economy"
)

// newPlayerState builds a ccg.State with one player entity and returns
// the state + entity ID for tests.
func newPlayerState() (*ccg.State, ccg.EntityID) {
	s := ccg.NewState()
	id := s.NewEntity("player", "0", nil)
	return s, id
}

func TestPoolCurrentDefaultsToZero(t *testing.T) {
	s, id := newPlayerState()
	gold := economy.Pool{Owner: id, Kind: "gold", Cap: 10}
	if got := gold.Current(s); got != 0 {
		t.Fatalf("fresh pool should have Current = 0, got %d", got)
	}
}

func TestPoolCurrentReadsCcgCounter(t *testing.T) {
	s, id := newPlayerState()
	s.AddCounter(id, "gold", 5)
	gold := economy.Pool{Owner: id, Kind: "gold", Cap: 10}
	if got := gold.Current(s); got != 5 {
		t.Fatalf("Current should reflect underlying counter, got %d (want 5)", got)
	}
}
