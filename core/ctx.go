package core

// Ctx is the engine-managed metadata that lives alongside G. It tells you
// whose turn it is, what phase the game is in, who can currently move, and
// whether the game is over. The fields mirror boardgame.io's `ctx`.
type Ctx struct {
	// Turn order
	NumPlayers    int      `json:"numPlayers"`
	CurrentPlayer string   `json:"currentPlayer"`
	PlayOrder     []string `json:"playOrder"`
	PlayOrderPos  int      `json:"playOrderPos"`
	Turn          int      `json:"turn"`

	// Phase routing — empty string means no active phase (parity with BGIO's
	// `null` phase).
	Phase string `json:"phase,omitempty"`

	// ActivePlayers is the set of players currently allowed to move, mapped
	// to the stage they're in (empty string means StageNull — active but not
	// in a stage). Nil means "only ctx.CurrentPlayer can move", which is the
	// default.
	ActivePlayers map[string]string `json:"activePlayers,omitempty"`

	// Gameover is nil while the game is in progress. Once Game.EndIf returns
	// a non-nil value, the engine writes it here and rejects further moves.
	// The value is game-specific (e.g. `{winner: "0"}` or `{draw: true}`).
	Gameover any `json:"gameover,omitempty"`

	// NumMoves is the number of moves played in the current turn, used to
	// enforce Turn.MinMoves / Turn.MaxMoves. Not visible in BGIO's docs but
	// kept in their internal state under the same purpose.
	NumMoves int `json:"_numMoves,omitempty"`
}
