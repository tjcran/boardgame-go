package stackduel

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/modules/ccg"
)

func applyOK(t *testing.T, g *core.Game, s core.State, player, move string, args ...any) core.State {
	t.Helper()
	next, err := core.Apply(g, s, core.MoveRequest{PlayerID: player, Move: move, Args: args})
	if err != nil {
		t.Fatalf("%s by %s: %v", move, player, err)
	}
	return next
}

// handCard finds the entity for a named card def in a player's hand.
func handCard(t *testing.T, s core.State, pid string, def ccg.DefID) ccg.EntityID {
	t.Helper()
	g := s.G.(*State)
	z, ok := g.Zones[handZone(pid)]
	if !ok {
		t.Fatalf("no hand zone for %s", pid)
	}
	for _, id := range z.Members {
		if e, ok := g.Get(id); ok && e.DefID == def {
			return id
		}
	}
	t.Fatalf("%s has no %q in hand: %v", pid, def, z.Members)
	return 0
}

func life(s core.State, pid string) int {
	return s.G.(*State).LifeOf(pid)
}

// passRound has both seats pass in priority order, closing the window
// and resolving exactly one stack object.
func passRound(t *testing.T, g *core.Game, s core.State, order ...string) core.State {
	t.Helper()
	for _, pid := range order {
		s = applyOK(t, g, s, pid, "pass")
	}
	return s
}

// TestFullDuelScript drives one full exchange of the duel end to end:
// summons with staged triggers, a cross-player battlefield trigger, a
// counterspell resolving LIFO, generic-cost payment, and turn handoff.
func TestFullDuelScript(t *testing.T) {
	game := New()
	s := core.NewMatch(game, 2, nil)

	// Turn 1, player 0. Glowmoth resolves unopposed.
	s = applyOK(t, game, s, "0", "cast", handCard(t, s, "0", "glowmoth"))
	if want := map[string]string{"0": respondStage}; s.Ctx.ActivePlayers["0"] != respondStage || len(s.Ctx.ActivePlayers) != 1 {
		t.Fatalf("window should gate the caster, ActivePlayers = %v want %v", s.Ctx.ActivePlayers, want)
	}
	// The non-holder is rejected by the engine, not game code.
	if _, err := core.Apply(game, s, core.MoveRequest{PlayerID: "1", Move: "pass"}); !errors.Is(err, core.ErrInactivePlayer) {
		t.Fatalf("out-of-window pass err = %v, want ErrInactivePlayer", err)
	}
	s = passRound(t, game, s, "0", "1")
	g := s.G.(*State)
	if z := g.Zones[fieldZone("0")]; len(z.Members) != 1 {
		t.Fatalf("glowmoth should be on 0's battlefield, members = %v", z.Members)
	}

	// Duskwisp: its enter trigger drains 1 from the opponent, and the
	// resident glowmoth's battlefield trigger gains 1 — both staged at
	// the event, flushed at the checkpoint, resolved LIFO through two
	// further priority rounds.
	s = applyOK(t, game, s, "0", "cast", handCard(t, s, "0", "duskwisp"))
	s = passRound(t, game, s, "0", "1") // resolves the summon; triggers hit the stack
	if got := len(s.G.(*State).PendingEffects); got != 2 {
		t.Fatalf("two staged triggers should be on the stack, got %d", got)
	}
	s = passRound(t, game, s, "0", "1") // resolves gain_life (flushed last = top)
	if got := life(s, "0"); got != startingLife+1 {
		t.Fatalf("life(0) after glowmoth trigger = %d, want %d", got, startingLife+1)
	}
	s = passRound(t, game, s, "0", "1") // resolves duskwisp's drain
	if got := life(s, "1"); got != startingLife-1 {
		t.Fatalf("life(1) after duskwisp trigger = %d, want %d", got, startingLife-1)
	}

	// Sunbolt gets countered: moonveil goes on top and resolves first,
	// removing the bolt — no damage happens.
	s = applyOK(t, game, s, "0", "cast", handCard(t, s, "0", "sunbolt"))
	s = applyOK(t, game, s, "0", "pass")
	s = applyOK(t, game, s, "1", "respond", handCard(t, s, "1", "moonveil"))
	s = passRound(t, game, s, "1", "0") // holder stays with the responder
	g = s.G.(*State)
	if len(g.PendingEffects) != 0 {
		t.Fatalf("counter should empty the stack, pending = %v", g.PendingEffects)
	}
	if got := life(s, "1"); got != startingLife-1 {
		t.Fatalf("countered bolt must not deal damage, life(1) = %d", got)
	}
	for pid, want := range map[string]int{"0": 1, "1": 1} {
		if z := g.Zones[graveZone(pid)]; len(z.Members) != want {
			t.Fatalf("graveyard:%s = %v, want %d card(s)", pid, z.Members, want)
		}
	}

	// Turn handoff; player 1 pays a pure-generic cost from colored
	// pools (PayWithGeneric) and player 0's moth triggers off the
	// OPPONENT's creature entering.
	s = applyOK(t, game, s, "0", "endTurn")
	if s.Ctx.CurrentPlayer != "1" {
		t.Fatalf("current player after endTurn = %s", s.Ctx.CurrentPlayer)
	}
	s = applyOK(t, game, s, "1", "cast", handCard(t, s, "1", "starling"))
	s = passRound(t, game, s, "1", "0") // summon resolves; moth trigger staged → stack
	s = passRound(t, game, s, "1", "0") // gain_life resolves
	if got := life(s, "0"); got != startingLife+2 {
		t.Fatalf("moth should trigger on the opponent's creature too, life(0) = %d want %d", got, startingLife+2)
	}
	if s.Ctx.ActivePlayers != nil {
		t.Fatalf("no window should remain open, ActivePlayers = %v", s.Ctx.ActivePlayers)
	}
}

// TestLethalEndsGame pins EndIf: when a resolution puts a player at 0
// life the engine ends the match with the opponent as winner.
func TestLethalEndsGame(t *testing.T) {
	game := New()
	s := core.NewMatch(game, 2, nil)
	g := s.G.(*State)
	// Put player 1 in bolt range.
	g.RemoveCounter(g.Players["1"], lifeKind, startingLife-2)

	s = applyOK(t, game, s, "0", "cast", handCard(t, s, "0", "sunbolt"))
	s = passRound(t, game, s, "0", "1")
	over, ok := s.Ctx.Gameover.(map[string]any)
	if !ok || over["winner"] != "0" {
		t.Fatalf("Gameover = %+v, want winner 0", s.Ctx.Gameover)
	}
}

// TestSorceryTimingEnforced pins the two speed rules: non-instants are
// not legal responses, and the turn can't end with a window open or a
// stack pending.
func TestSorceryTimingEnforced(t *testing.T) {
	game := New()
	s := core.NewMatch(game, 2, nil)
	s = applyOK(t, game, s, "0", "cast", handCard(t, s, "0", "duskwisp"))

	if _, err := core.Apply(game, s, core.MoveRequest{
		PlayerID: "0", Move: "respond", Args: []any{any(handCard(t, s, "0", "glowmoth"))},
	}); !errors.Is(err, errNotInstant) {
		t.Fatalf("responding with a creature err = %v, want errNotInstant", err)
	}
	if _, err := core.Apply(game, s, core.MoveRequest{PlayerID: "0", Move: "cast",
		Args: []any{any(handCard(t, s, "0", "glowmoth"))}}); !errors.Is(err, errWindowOpen) {
		t.Fatalf("sorcery cast during window err = %v, want errWindowOpen", err)
	}
	if _, err := core.Apply(game, s, core.MoveRequest{PlayerID: "0", Move: "endTurn"}); !errors.Is(err, errWindowOpen) {
		t.Fatalf("endTurn during window err = %v, want errWindowOpen", err)
	}
}

// TestReplayDeterminism applies the same move log twice and requires
// byte-identical serialized states — the whole stack/priority/trigger
// pipeline must add no hidden nondeterminism.
func TestReplayDeterminism(t *testing.T) {
	script := func() core.State {
		game := New()
		s := core.NewMatch(game, 2, nil)
		s = applyOK(t, game, s, "0", "cast", handCard(t, s, "0", "glowmoth"))
		s = passRound(t, game, s, "0", "1")
		s = applyOK(t, game, s, "0", "cast", handCard(t, s, "0", "duskwisp"))
		s = passRound(t, game, s, "0", "1")
		s = passRound(t, game, s, "0", "1")
		s = passRound(t, game, s, "0", "1")
		s = applyOK(t, game, s, "0", "endTurn")
		s = applyOK(t, game, s, "1", "cast", handCard(t, s, "1", "starling"))
		s = passRound(t, game, s, "1", "0")
		s = passRound(t, game, s, "1", "0")
		return s
	}
	a, err := json.Marshal(script().G)
	if err != nil {
		t.Fatalf("marshal a: %v", err)
	}
	b, err := json.Marshal(script().G)
	if err != nil {
		t.Fatalf("marshal b: %v", err)
	}
	if string(a) != string(b) {
		t.Fatalf("identical scripts serialized differently:\n%s\n%s", a, b)
	}
}

// TestRestoreMidWindow round-trips the full G through JSON while a
// priority window is OPEN and a summon is pending, rebuilds it with
// DecodeG (which re-wires the bus subscribers and battlefield
// bindings), and plays the rest of the exchange to the same outcome —
// the serialize/restore path a Postgres-backed match would take.
func TestRestoreMidWindow(t *testing.T) {
	game := New()
	s := core.NewMatch(game, 2, nil)

	// Glowmoth resolves; its trigger is a live BindAbility.
	s = applyOK(t, game, s, "0", "cast", handCard(t, s, "0", "glowmoth"))
	s = passRound(t, game, s, "0", "1")
	// Duskwisp cast leaves the window OPEN with the summon pending.
	s = applyOK(t, game, s, "0", "cast", handCard(t, s, "0", "duskwisp"))

	raw, err := json.Marshal(s.G)
	if err != nil {
		t.Fatalf("marshal mid-window: %v", err)
	}
	restoredG, err := game.DecodeG(raw)
	if err != nil {
		t.Fatalf("DecodeG: %v", err)
	}
	restored := restoredG.(*State)
	if !restored.Priority.IsOpen() || restored.Priority.Holder() != "0" {
		t.Fatalf("restored window lost: %+v", restored.Priority)
	}
	s.G = restored

	// The exchange continues on the restored state. Both triggers must
	// still fire — proving DecodeG re-registered the global router AND
	// re-bound the battlefield glowmoth from serialized zone data.
	s = passRound(t, game, s, "0", "1") // summon resolves, triggers stack
	s = passRound(t, game, s, "0", "1") // gain_life
	s = passRound(t, game, s, "0", "1") // drain
	if got := life(s, "0"); got != startingLife+1 {
		t.Errorf("life(0) after restore = %d, want %d (glowmoth rebind lost?)", got, startingLife+1)
	}
	if got := life(s, "1"); got != startingLife-1 {
		t.Errorf("life(1) after restore = %d, want %d (router rebind lost?)", got, startingLife-1)
	}
}
