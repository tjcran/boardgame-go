package core

import "strconv"

// NewMatch builds the starting State for a fresh match of the given Game with
// numPlayers seats. It calls the Game's Setup to produce G and initialises Ctx
// with a default 0..N-1 PlayOrder, current player "0", turn 1.
func NewMatch(game *Game, numPlayers int) State {
	n := game.playerCount(numPlayers)
	order := make([]string, n)
	for i := 0; i < n; i++ {
		order[i] = strconv.Itoa(i)
	}

	var g G
	if game.Setup != nil {
		g = game.Setup(n)
	}

	return State{
		G: g,
		Ctx: Ctx{
			NumPlayers:    n,
			CurrentPlayer: order[0],
			PlayOrder:     order,
			Turn:          1,
		},
	}
}
