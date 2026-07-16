package ccg_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/modules/ccg"
)

// mcFor builds the minimal MoveContext the protocol needs for
// pure state-machine tests (stage "" = no engine wiring).
func mcFor(player string) *core.MoveContext {
	return &core.MoveContext{PlayerID: player, Events: &core.Events{}}
}

func TestOpenWindowValidatesOrder(t *testing.T) {
	cases := []struct {
		name    string
		players []string
	}{
		{"empty", nil},
		{"duplicate", []string{"0", "1", "0"}},
		{"blank id", []string{"0", ""}},
	}
	for _, c := range cases {
		p := &ccg.PriorityState{}
		if err := p.OpenWindow(mcFor("0"), c.players, ""); !errors.Is(err, ccg.ErrBadPriorityOrder) {
			t.Errorf("%s: err = %v, want ErrBadPriorityOrder", c.name, err)
		}
		if p.IsOpen() {
			t.Errorf("%s: rejected open must leave window closed", c.name)
		}
	}
}

func TestOpenWindowTwiceErrors(t *testing.T) {
	p := &ccg.PriorityState{}
	if err := p.OpenWindow(mcFor("0"), []string{"0", "1"}, ""); err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := p.OpenWindow(mcFor("0"), []string{"0", "1"}, ""); !errors.Is(err, ccg.ErrPriorityOpen) {
		t.Errorf("second open err = %v, want ErrPriorityOpen", err)
	}
}

func TestPassRotatesAndClosesOnAllPass(t *testing.T) {
	p := &ccg.PriorityState{}
	if err := p.OpenWindow(mcFor("0"), []string{"0", "1", "2"}, ""); err != nil {
		t.Fatalf("open: %v", err)
	}
	if p.Holder() != "0" {
		t.Fatalf("initial holder = %q, want 0", p.Holder())
	}
	closed, err := p.Pass(mcFor("0"))
	if err != nil || closed {
		t.Fatalf("pass 1: closed=%v err=%v", closed, err)
	}
	if p.Holder() != "1" {
		t.Fatalf("holder after pass = %q, want 1", p.Holder())
	}
	closed, _ = p.Pass(mcFor("1"))
	if closed {
		t.Fatal("pass 2 should not close a 3-player window")
	}
	closed, err = p.Pass(mcFor("2"))
	if err != nil || !closed {
		t.Fatalf("third consecutive pass should close: closed=%v err=%v", closed, err)
	}
	if p.IsOpen() || p.Holder() != "" {
		t.Errorf("closed window should be zeroed: %+v", p)
	}
}

func TestActionResetsConsecutivePasses(t *testing.T) {
	p := &ccg.PriorityState{}
	p.OpenWindow(mcFor("0"), []string{"0", "1"}, "")
	p.Pass(mcFor("0")) // holder 1, passes=1
	if err := p.ActionTaken(mcFor("1"), true); err != nil {
		t.Fatalf("action: %v", err)
	}
	if p.Holder() != "1" {
		t.Errorf("retain=true must keep priority with the actor, holder = %q", p.Holder())
	}
	// The earlier pass no longer counts toward closure.
	closed, _ := p.Pass(mcFor("1"))
	if closed {
		t.Fatal("window closed after a single post-action pass; ActionTaken failed to reset")
	}
	closed, _ = p.Pass(mcFor("0"))
	if !closed {
		t.Fatal("both players passed since last action; window should close")
	}
}

func TestActionTakenWithoutRetainRotates(t *testing.T) {
	p := &ccg.PriorityState{}
	p.OpenWindow(mcFor("0"), []string{"0", "1"}, "")
	if err := p.ActionTaken(mcFor("0"), false); err != nil {
		t.Fatalf("action: %v", err)
	}
	if p.Holder() != "1" {
		t.Errorf("retain=false should rotate, holder = %q", p.Holder())
	}
}

func TestWrongHolderAndClosedWindowErrors(t *testing.T) {
	p := &ccg.PriorityState{}
	if _, err := p.Pass(mcFor("0")); !errors.Is(err, ccg.ErrPriorityClosed) {
		t.Errorf("Pass on closed window err = %v, want ErrPriorityClosed", err)
	}
	if err := p.ActionTaken(mcFor("0"), true); !errors.Is(err, ccg.ErrPriorityClosed) {
		t.Errorf("ActionTaken on closed window err = %v, want ErrPriorityClosed", err)
	}
	p.OpenWindow(mcFor("0"), []string{"0", "1"}, "")
	if _, err := p.Pass(mcFor("1")); !errors.Is(err, ccg.ErrNotPriorityHolder) {
		t.Errorf("Pass by non-holder err = %v, want ErrNotPriorityHolder", err)
	}
	if err := p.ActionTaken(mcFor("1"), true); !errors.Is(err, ccg.ErrNotPriorityHolder) {
		t.Errorf("ActionTaken by non-holder err = %v, want ErrNotPriorityHolder", err)
	}
	if p.Holder() != "0" {
		t.Errorf("failed calls must not advance the window, holder = %q", p.Holder())
	}
}

func TestDropNonHolderAdjustsIndex(t *testing.T) {
	p := &ccg.PriorityState{}
	p.OpenWindow(mcFor("0"), []string{"0", "1", "2"}, "")
	p.Pass(mcFor("0"))
	p.Pass(mcFor("1")) // holder now 2, passes=2
	closed, err := p.Drop(mcFor("2"), "0")
	if err != nil {
		t.Fatalf("drop: %v", err)
	}
	// Order shrank to [1 2]; passes=2 >= 2 → the remaining players'
	// passes suffice, window closes.
	if !closed {
		t.Fatal("threshold shrink should close the window")
	}
}

func TestDropHolderRotates(t *testing.T) {
	p := &ccg.PriorityState{}
	p.OpenWindow(mcFor("0"), []string{"0", "1", "2"}, "")
	closed, err := p.Drop(mcFor("0"), "0")
	if err != nil || closed {
		t.Fatalf("drop holder: closed=%v err=%v", closed, err)
	}
	if p.Holder() != "1" {
		t.Errorf("holder after dropping the holder = %q, want 1", p.Holder())
	}
	// Wrap-around: drop the last-index holder.
	p2 := &ccg.PriorityState{}
	p2.OpenWindow(mcFor("0"), []string{"0", "1"}, "")
	p2.Pass(mcFor("0")) // holder 1 (last index)
	// A pass then an action so passes=0 and holder=1.
	p2.ActionTaken(mcFor("1"), true)
	closed, err = p2.Drop(mcFor("1"), "1")
	if err != nil || closed {
		t.Fatalf("drop wrap: closed=%v err=%v", closed, err)
	}
	if p2.Holder() != "0" {
		t.Errorf("holder should wrap to 0, got %q", p2.Holder())
	}
}

func TestDropLastPlayerCloses(t *testing.T) {
	p := &ccg.PriorityState{}
	p.OpenWindow(mcFor("0"), []string{"0"}, "")
	closed, err := p.Drop(mcFor("0"), "0")
	if err != nil || !closed {
		t.Fatalf("dropping the only player must close: closed=%v err=%v", closed, err)
	}
}

func TestDropUnknownPlayerErrors(t *testing.T) {
	p := &ccg.PriorityState{}
	p.OpenWindow(mcFor("0"), []string{"0", "1"}, "")
	if _, err := p.Drop(mcFor("0"), "9"); !errors.Is(err, ccg.ErrNotInPriorityOrder) {
		t.Errorf("drop of outsider err = %v, want ErrNotInPriorityOrder", err)
	}
}

func TestCloseWindowAndReset(t *testing.T) {
	p := &ccg.PriorityState{}
	p.OpenWindow(mcFor("0"), []string{"0", "1"}, "")
	p.CloseWindow(mcFor("0"))
	if p.IsOpen() {
		t.Fatal("CloseWindow left window open")
	}
	p.CloseWindow(mcFor("0")) // idempotent

	p.OpenWindow(mcFor("0"), []string{"0", "1"}, "")
	p.Reset()
	if p.IsOpen() || len(p.Order) != 0 {
		t.Errorf("Reset should zero the struct: %+v", p)
	}
}

func TestHaltWhileOpen(t *testing.T) {
	p := &ccg.PriorityState{}
	pick := ccg.HaltWhileOpen(p, ccg.PickBack)
	pending := []ccg.Effect{{Kind: "a"}, {Kind: "b"}}
	p.OpenWindow(mcFor("0"), []string{"0", "1"}, "")
	if got := pick(pending); got != -1 {
		t.Errorf("picker while open = %d, want -1", got)
	}
	p.Reset()
	if got := pick(pending); got != 1 {
		t.Errorf("picker after close = %d, want 1 (PickBack)", got)
	}
}

func TestPriorityStateJSONRoundTripMidWindow(t *testing.T) {
	p := &ccg.PriorityState{}
	p.OpenWindow(mcFor("0"), []string{"0", "1", "2"}, "respond")
	p.Pass(mcFor("0")) // holder 1, passes 1
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	restored := &ccg.PriorityState{}
	if err := json.Unmarshal(raw, restored); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(p, restored) {
		t.Fatalf("round-trip diverged:\n%+v\n%+v", p, restored)
	}
	// The restored window keeps working.
	closed, err := restored.Pass(mcFor("1"))
	if err != nil || closed {
		t.Fatalf("restored window broken: closed=%v err=%v", closed, err)
	}
	if restored.Holder() != "2" {
		t.Errorf("restored holder rotation = %q, want 2", restored.Holder())
	}
}

// ---------------------------------------------------------------------------
// Engine integration: a 3-player game where a spell opens a window,
// responses stack LIFO, and the all-pass close resolves the stack.
// ---------------------------------------------------------------------------

type prioG struct {
	// Named field, not embedded: ccg.State's custom MarshalJSON would
	// otherwise hijack the whole struct's encoding.
	CCG      *ccg.State        `json:"ccg"`
	Priority ccg.PriorityState `json:"priority"`
	Resolved []string          `json:"resolved,omitempty"`
}

func prioGame() *core.Game {
	table := ccg.ResolverTable{}
	resolveAs := func(kind string) ccg.Resolver {
		return func(s *ccg.State, eff ccg.Effect) error { return nil }
	}
	for _, kind := range []string{"bolt", "counter", "fog"} {
		table[kind] = resolveAs(kind)
	}

	drainIfClosed := func(g *prioG) {
		pick := ccg.HaltWhileOpen(&g.Priority, ccg.PickBack)
		for {
			eff, ok, err := g.CCG.ResolveNext(pick, table)
			if err != nil || !ok {
				return
			}
			g.Resolved = append(g.Resolved, eff.Kind)
		}
	}

	return &core.Game{
		Name:       "priority-integration",
		MinPlayers: 3,
		MaxPlayers: 3,
		Setup: func(_ core.Ctx, _ any) core.G {
			return &prioG{CCG: ccg.NewState()}
		},
		Moves: map[string]any{
			// The current player starts the exchange: pushes a spell
			// and opens the window over the play order.
			"open": core.MoveFn(func(mc *core.MoveContext, args ...any) (core.G, error) {
				g := mc.G.(*prioG)
				g.CCG.PushEffect(ccg.Effect{Kind: args[0].(string), Controller: mc.PlayerID})
				if err := g.Priority.OpenWindow(mc, mc.Ctx.PlayOrder, "respond"); err != nil {
					return nil, err
				}
				return g, nil
			}),
		},
		Turn: &core.TurnConfig{
			Stages: map[string]*core.StageConfig{
				"respond": {
					Moves: map[string]any{
						"respond": core.MoveFn(func(mc *core.MoveContext, args ...any) (core.G, error) {
							g := mc.G.(*prioG)
							g.CCG.PushEffect(ccg.Effect{Kind: args[0].(string), Controller: mc.PlayerID})
							if err := g.Priority.ActionTaken(mc, true); err != nil {
								return nil, err
							}
							return g, nil
						}),
						"pass": core.MoveFn(func(mc *core.MoveContext, _ ...any) (core.G, error) {
							g := mc.G.(*prioG)
							closed, err := g.Priority.Pass(mc)
							if err != nil {
								return nil, err
							}
							if closed {
								drainIfClosed(g)
							}
							return g, nil
						}),
					},
				},
			},
		},
	}
}

func applyOK(t *testing.T, g *core.Game, s core.State, player, move string, args ...any) core.State {
	t.Helper()
	next, err := core.Apply(g, s, core.MoveRequest{PlayerID: player, Move: move, Args: args})
	if err != nil {
		t.Fatalf("%s by %s: %v", move, player, err)
	}
	return next
}

func TestPriorityEngineIntegration(t *testing.T) {
	g := prioGame()
	s := core.NewMatch(g, 3, nil)

	// Player 0 casts bolt and opens the window.
	s = applyOK(t, g, s, "0", "open", "bolt")
	if !reflect.DeepEqual(s.Ctx.ActivePlayers, map[string]string{"0": "respond"}) {
		t.Fatalf("ActivePlayers after open = %v, want {0:respond}", s.Ctx.ActivePlayers)
	}

	// An out-of-window player is rejected by the engine itself.
	if _, err := core.Apply(g, s, core.MoveRequest{PlayerID: "1", Move: "pass"}); !errors.Is(err, core.ErrInactivePlayer) {
		t.Fatalf("out-of-window move err = %v, want ErrInactivePlayer", err)
	}

	// 0 passes; 1 responds with counter (goes on top) and retains; then
	// 1, 2, and 0 pass in unbroken succession — three passes for three
	// players closes the window.
	s = applyOK(t, g, s, "0", "pass")
	if !reflect.DeepEqual(s.Ctx.ActivePlayers, map[string]string{"1": "respond"}) {
		t.Fatalf("ActivePlayers after pass = %v, want {1:respond}", s.Ctx.ActivePlayers)
	}
	s = applyOK(t, g, s, "1", "respond", "counter")
	if !reflect.DeepEqual(s.Ctx.ActivePlayers, map[string]string{"1": "respond"}) {
		t.Fatalf("retain should keep the actor gated, got %v", s.Ctx.ActivePlayers)
	}
	s = applyOK(t, g, s, "1", "pass")
	s = applyOK(t, g, s, "2", "pass")
	s = applyOK(t, g, s, "0", "pass")

	final := s.G.(*prioG)
	if !reflect.DeepEqual(final.Resolved, []string{"counter", "bolt"}) {
		t.Errorf("LIFO resolution order = %v, want [counter bolt]", final.Resolved)
	}
	if s.Ctx.ActivePlayers != nil {
		t.Errorf("close should restore the default active set, got %v", s.Ctx.ActivePlayers)
	}
	// Default gate restored: the current player may move again.
	s = applyOK(t, g, s, "0", "open", "fog")
	if final := s.G.(*prioG); !final.Priority.IsOpen() {
		t.Errorf("window should reopen after restore")
	}
}

// The same move log applied twice must yield deeply equal states —
// the protocol adds no hidden nondeterminism.
func TestPriorityReplayDeterminism(t *testing.T) {
	script := func() core.State {
		g := prioGame()
		s := core.NewMatch(g, 3, nil)
		s = applyOK(t, g, s, "0", "open", "bolt")
		s = applyOK(t, g, s, "0", "pass")
		s = applyOK(t, g, s, "1", "respond", "counter")
		s = applyOK(t, g, s, "1", "pass")
		s = applyOK(t, g, s, "2", "pass")
		s = applyOK(t, g, s, "0", "pass")
		return s
	}
	a, b := script(), script()
	if !reflect.DeepEqual(a.G, b.G) {
		t.Errorf("G diverged across identical runs:\n%+v\n%+v", a.G, b.G)
	}
	if !reflect.DeepEqual(a.Ctx.ActivePlayers, b.Ctx.ActivePlayers) {
		t.Errorf("ActivePlayers diverged: %v vs %v", a.Ctx.ActivePlayers, b.Ctx.ActivePlayers)
	}
}
