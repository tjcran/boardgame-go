package match

import (
	"testing"

	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/storage"
)

func seedMgrGame() *core.Game {
	return &core.Game{
		Name:       "seed-mgr",
		MinPlayers: 1,
		MaxPlayers: 2,
		Setup:      func(ctx core.Ctx, _ any) core.G { return map[string]any{} },
		Moves: map[string]any{
			"noop": core.MoveFn(func(mc *core.MoveContext, _ ...any) (core.G, error) { return mc.G, nil }),
		},
	}
}

func TestCreateAssignsSecretSeed(t *testing.T) {
	mgr := NewManager(storage.NewMemory())
	mgr.MustRegister(seedMgrGame())

	id, err := mgr.Create("seed-mgr", CreateOptions{NumPlayers: 2})
	if err != nil {
		t.Fatal(err)
	}
	st, err := mgr.State(id)
	if err != nil {
		t.Fatal(err)
	}
	if st.State.Ctx.Seed == 0 {
		t.Fatal("Create must assign a non-zero per-match seed")
	}

	id2, err := mgr.Create("seed-mgr", CreateOptions{NumPlayers: 2})
	if err != nil {
		t.Fatal(err)
	}
	st2, _ := mgr.State(id2)
	if st2.State.Ctx.Seed == st.State.Ctx.Seed {
		t.Fatal("two matches got the same seed")
	}
}

func TestSeedFnInjectable(t *testing.T) {
	mgr := NewManager(storage.NewMemory())
	mgr.MustRegister(seedMgrGame())
	mgr.SeedFn = func() uint64 { return 1234 }

	id, err := mgr.Create("seed-mgr", CreateOptions{NumPlayers: 2})
	if err != nil {
		t.Fatal(err)
	}
	st, _ := mgr.State(id)
	if st.State.Ctx.Seed != 1234 {
		t.Fatalf("Seed = %d, want injected 1234", st.State.Ctx.Seed)
	}
}

func TestExportImportCarriesSeed(t *testing.T) {
	mgr := NewManager(storage.NewMemory())
	mgr.MustRegister(seedMgrGame())
	mgr.SeedFn = func() uint64 { return 777 }

	id, err := mgr.Create("seed-mgr", CreateOptions{NumPlayers: 1})
	if err != nil {
		t.Fatal(err)
	}
	jr, err := mgr.Join(id, "p", JoinOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.MoveReq(id, jr.PlayerID, jr.PlayerCredentials, core.MoveRequest{Move: "noop"}); err != nil {
		t.Fatal(err)
	}

	exp, err := mgr.ExportMatch(id)
	if err != nil {
		t.Fatal(err)
	}
	if exp.Seed != 777 {
		t.Fatalf("export Seed = %d, want 777", exp.Seed)
	}

	newID, err := mgr.ImportMatch(exp)
	if err != nil {
		t.Fatal(err)
	}
	st, _ := mgr.State(newID)
	if st.State.Ctx.Seed != 777 {
		t.Fatalf("imported match Seed = %d, want 777 (replay must be seeded)", st.State.Ctx.Seed)
	}
}
