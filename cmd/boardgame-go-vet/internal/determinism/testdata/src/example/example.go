package example

import (
	"math/rand"
	"time"

	"core"
)

// goodMove uses the engine's PRNG — no diagnostic expected.
func goodMove(mc *core.MoveContext, args ...any) (core.G, error) {
	_ = mc.Random.D6()
	return mc.G, nil
}

// badMoveTime hits time.Now from a MoveFn — diagnostic expected.
func badMoveTime(mc *core.MoveContext, args ...any) (core.G, error) {
	_ = time.Now() // want `determinism: time.Now is not allowed in a MoveFn body`
	return mc.G, nil
}

// badMoveRand uses math/rand directly — diagnostic expected.
func badMoveRand(mc *core.MoveContext, args ...any) (core.G, error) {
	_ = rand.Intn(6) // want `determinism: math/rand.Intn is not allowed in a MoveFn body`
	return mc.G, nil
}

// badHook is a HookFn calling time.Since.
func badHook(mc *core.MoveContext) core.G {
	_ = time.Since(time.Time{}) // want `determinism: time.Since is not allowed in a HookFn body`
	return mc.G
}

// nonMoveFunc has the wrong signature — should NOT be flagged.
func nonMoveFunc() {
	_ = time.Now()
	_ = rand.Intn(6)
}
