package core

// Random is the seeded PRNG exposed to moves and hooks via MoveContext.
// The implementation lands with the Random plugin (parity task #18). Until
// then the type exists as a stub so the engine compiles and games can
// declare moves with the canonical signature.
type Random struct {
	// state intentionally unset until the Random plugin is wired.
	state any
}
