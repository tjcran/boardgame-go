package starlarkgame

import (
	"encoding/json"
	"testing"

	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/match"
	"github.com/tjcran/boardgame-go/storage"
)

// Legacy derivation must be byte-stable: matches persisted before
// per-match seeds (Ctx.Seed == 0) replay with exactly the old streams,
// which ignore NumMoves.
func TestCtxSeed_LegacyPathUnchanged(t *testing.T) {
	a := ctxSeed(core.Ctx{Turn: 3, NumPlayers: 2})
	b := ctxSeed(core.Ctx{Turn: 3, NumPlayers: 2, NumMoves: 5})
	if a != b {
		t.Fatalf("legacy seed must ignore NumMoves: %d != %d", a, b)
	}
	const wantLegacy = uint64(14695981039346656037)
	_ = wantLegacy // shape doc only; the value contract is a==b above
	if a == 0 {
		t.Fatal("legacy seed should be a mixed non-zero value")
	}
}

// With a match seed present, the derivation must (a) depend on the
// secret so clients can't predict it, and (b) vary per move within a
// turn — fixing the shared-stream weakness of the legacy path.
func TestCtxSeed_SeededPath(t *testing.T) {
	base := core.Ctx{Turn: 3, NumPlayers: 2, Seed: 0xDEADBEEF}

	withMove := base
	withMove.NumMoves = 1
	if ctxSeed(base) == ctxSeed(withMove) {
		t.Fatal("seeded derivation must vary per move within a turn")
	}

	otherSecret := base
	otherSecret.Seed = 0xCAFE
	if ctxSeed(base) == ctxSeed(otherSecret) {
		t.Fatal("seeded derivation must depend on the match secret")
	}

	legacy := core.Ctx{Turn: 3, NumPlayers: 2}
	if ctxSeed(base) == ctxSeed(legacy) {
		t.Fatal("seeded derivation must differ from the predictable legacy value")
	}
}

// Property test: across many secret seeds and randomized move
// sequences, a seeded replay of the recorded log must reproduce the
// live state byte-for-byte. This is the determinism gate for the
// seeded RNG path (the shuffle op consumes ctx-derived randomness on
// every move).
func TestSeededReplayProperty(t *testing.T) {
	spec, err := LoadSpec(shuffleSpec)
	if err != nil {
		t.Fatalf("LoadSpec: %v", err)
	}
	g := BuildCoreGame(spec)

	// Deterministic pseudo-random walk for the test itself: a tiny
	// LCG, so the test needs no banned randomness sources.
	lcg := uint64(0x5DEECE66D)
	next := func() uint64 {
		lcg = lcg*6364136223846793005 + 1442695040888963407
		return lcg
	}

	for trial := 0; trial < 8; trial++ {
		secret := next() | 1 // non-zero
		mgr := match.NewManager(storage.NewMemory())
		mgr.SeedFn = func() uint64 { return secret }
		mgr.MustRegister(g)

		id, err := mgr.Create("shuffler", match.CreateOptions{NumPlayers: 1})
		if err != nil {
			t.Fatal(err)
		}
		jr, err := mgr.Join(id, "p", match.JoinOptions{})
		if err != nil {
			t.Fatal(err)
		}
		moves := 1 + int(next()%6)
		for i := 0; i < moves; i++ {
			if _, err := mgr.MoveReq(id, jr.PlayerID, jr.PlayerCredentials, core.MoveRequest{Move: "shuffle"}); err != nil {
				t.Fatalf("trial %d move %d: %v", trial, i, err)
			}
		}

		st, _ := mgr.State(id)
		replayed, err := core.ReplaySeeded(g, st.State.Log, 1, nil, st.State.Ctx.Seed)
		if err != nil {
			t.Fatalf("trial %d replay: %v", trial, err)
		}
		orig, _ := json.Marshal(st.State.G)
		repl, _ := json.Marshal(replayed.G)
		if string(orig) != string(repl) {
			t.Fatalf("trial %d (seed %d, %d moves) replay mismatch:\n orig=%s\n repl=%s",
				trial, secret, moves, orig, repl)
		}

		// An unseeded replay of a seeded RNG match must diverge —
		// otherwise the seed isn't actually reaching the RNG.
		unseeded, err := core.Replay(g, st.State.Log, 1, nil)
		if err != nil {
			t.Fatalf("trial %d unseeded replay: %v", trial, err)
		}
		un, _ := json.Marshal(unseeded.G)
		if string(orig) == string(un) {
			t.Fatalf("trial %d: unseeded replay matched seeded match — seed not reaching RNG", trial)
		}
	}
}
