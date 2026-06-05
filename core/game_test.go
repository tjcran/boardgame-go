package core

import "testing"

func TestPlayerCountClampsToGameBounds(t *testing.T) {
	bounded := &Game{Name: "b", MinPlayers: 2, MaxPlayers: 4}
	unbounded := &Game{Name: "u"}

	cases := []struct {
		name      string
		game      *Game
		requested int
		want      int
	}{
		{"bounded/default-uses-max", bounded, 0, 4},
		{"bounded/in-range-passthrough", bounded, 3, 3},
		{"bounded/above-max-clamps-down", bounded, 5, 4},
		{"bounded/below-min-clamps-up", bounded, 1, 2},
		{"unbounded/default-is-two", unbounded, 0, 2},
		{"unbounded/passthrough", unbounded, 7, 7},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.game.PlayerCount(tc.requested); got != tc.want {
				t.Fatalf("PlayerCount(%d) = %d, want %d", tc.requested, got, tc.want)
			}
		})
	}
}
