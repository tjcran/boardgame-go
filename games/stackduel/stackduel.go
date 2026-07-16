// Package stackduel is a two-player spell duel built on the boardgame-go
// engine. It is the reference example for the *stack + priority* pattern:
// every spell goes onto a LIFO stack as a pending ccg.Effect, each cast
// opens a priority window in which both players may respond or pass, and
// an unbroken round of passes resolves exactly one stack object before
// priority reopens.
//
// The pattern composes from existing primitives — no engine changes:
//
//   - ccg.PriorityState runs the pass-loop protocol. Each rotation gates
//     the holder into the "respond" stage via SetActivePlayers, so the
//     engine itself rejects out-of-window moves and the stage move table
//     scopes what a response may be.
//   - The stack is ccg.State.PendingEffects ordered by ccg.PickBack
//     (tail = top), halted by ccg.HaltWhileOpen while a window is open.
//   - A counterspell is itself a stack object: its resolver runs first
//     (LIFO) and calls RemoveEffect on its target — countering a counter
//     works with no extra machinery.
//   - Triggered abilities never push mid-Publish: handlers call
//     StageTrigger, and the move that owns the checkpoint flushes them in
//     APNAP order via FlushTriggers + OrderByPlayer. Glowmoth's
//     battlefield trigger uses BindAbility (auto-unbinds when it leaves);
//     Duskwisp's enter-the-battlefield trigger uses a global subscriber.
//   - Colored costs are economy.Pools ("sun" / "moon" counters on a
//     per-player entity) paid atomically with economy.Basket; generic
//     cost components use PayWithGeneric with a caller-chosen fallback
//     order. Life is a plain ccg counter on the same entity, so
//     resolvers need nothing beyond *ccg.State.
//
// Decks are fixed lists dealt in a deterministic order so replay tests
// need no seed plumbing; a real game would ctx-seed Shuffle instead.
package stackduel

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/modules/ccg"
	"github.com/tjcran/boardgame-go/modules/economy"
)

const (
	startingLife = 8
	manaPerTurn  = 3
	openingHand  = 4

	// respondStage is the stage each priority rotation gates the holder
	// into; its move table is the complete set of legal responses.
	respondStage = "respond"

	stackZone = ccg.ZoneName("stack")
	lifeKind  = "life"
)

// State is the stackduel G payload. Embedding *ccg.State gives it the
// entity/zone/effect bookkeeping and (since ccg carries no custom JSON
// codec) still round-trips the game fields below.
type State struct {
	*ccg.State

	// Priority is the pass-loop for the currently open response window.
	Priority ccg.PriorityState `json:"priority"`

	// Players maps seat → the per-player entity whose counters hold the
	// sun/moon mana pools and the life total.
	Players map[string]ccg.EntityID `json:"players"`
}

// --- card definitions -------------------------------------------------

// spec is the code side of a card: how to cost it and what pending
// effect casting it pushes. Kept in a package table keyed by DefID —
// defs carry the data (cost, flags), specs carry the verbs.
type spec struct {
	cost    economy.Cost
	generic int
	instant bool // castable in response windows, not only at sorcery speed
	kind    string
	data    map[string]any
}

var specs = map[ccg.DefID]spec{
	"sunbolt":   {cost: economy.Cost{"sun": 1}, instant: true, kind: "damage", data: map[string]any{"amount": 2}},
	"emberfall": {cost: economy.Cost{"sun": 2}, generic: 1, kind: "damage", data: map[string]any{"amount": 4}},
	"moonveil":  {cost: economy.Cost{"moon": 1}, instant: true, kind: "counter"},
	"lunartide": {cost: economy.Cost{"moon": 1}, instant: true, kind: "gain_life", data: map[string]any{"amount": 2}},
	"eclipse":   {cost: economy.Cost{"sun": 1, "moon": 1}, kind: "drain", data: map[string]any{"amount": 2}},
	"glowmoth":  {cost: economy.Cost{"sun": 1}, kind: "summon"},
	"duskwisp":  {cost: economy.Cost{"moon": 1}, kind: "summon"},
	"starling":  {generic: 2, kind: "summon"},
}

// deckOrder is each player's fixed deck, bottom to top: Draw pops the
// END of the slice, so the LAST entries here are the opening hand.
var deckOrder = []ccg.DefID{
	"eclipse", "emberfall", "lunartide", "starling", // library
	"duskwisp", "glowmoth", "moonveil", "sunbolt", // opening hand
}

func catalog() *ccg.Catalog {
	c := ccg.NewCatalog()
	for id, sp := range specs {
		if err := c.Register(ccg.CardDef{ID: id, Type: sp.kind, BaseAttrs: map[string]any{"instant": sp.instant}}); err != nil {
			panic(fmt.Sprintf("stackduel: duplicate def %q: %v", id, err))
		}
	}
	return c
}

// --- setup / wiring ---------------------------------------------------

// New returns the registered Game definition. Pass to a match.Manager.
func New() *core.Game {
	return &core.Game{
		Name:       "stackduel",
		MinPlayers: 2,
		MaxPlayers: 2,
		Setup:      setup,
		DecodeG:    decodeG,
		Moves: map[string]any{
			// Sorcery-speed cast by the turn player; opens the window.
			"cast": core.MoveFn(cast),
			// Ends the turn once the stack is empty and no window is open.
			"endTurn": core.MoveFn(endTurn),
		},
		Turn: &core.TurnConfig{
			OnBegin: turnBegin,
			Stages: map[string]*core.StageConfig{
				respondStage: {
					Moves: map[string]any{
						"respond": core.MoveFn(respond),
						"pass":    core.MoveFn(pass),
					},
				},
			},
		},
		EndIf: endIf,
	}
}

func setup(ctx core.Ctx, _ any) core.G {
	g := &State{
		State:   ccg.NewState(),
		Players: map[string]ccg.EntityID{},
	}
	cat := catalog()
	g.NewZone(stackZone, true)
	for _, pid := range ctx.PlayOrder {
		g.Players[pid] = g.NewEntity("player", pid, nil)
		g.AddCounter(g.Players[pid], lifeKind, startingLife)
		for _, kind := range []string{"sun", "moon"} {
			pool(g, pid, kind).Set(g.State, manaPerTurn)
		}
		g.NewZone(handZone(pid), false)
		g.NewZone(deckZone(pid), true)
		g.NewZone(fieldZone(pid), false)
		g.NewZone(graveZone(pid), false)
		for _, def := range deckOrder {
			id, err := g.Instantiate(cat, def, pid)
			if err != nil {
				panic(fmt.Sprintf("stackduel: setup instantiate %q: %v", def, err))
			}
			if err := g.Add(deckZone(pid), id); err != nil {
				panic(fmt.Sprintf("stackduel: setup deal: %v", err))
			}
		}
		drawCards(g, pid, openingHand)
	}
	wire(g)
	return g
}

// decodeG reconstructs the typed G after a serializing store load and
// re-registers the process-local subscribers — the bus routing table
// (global subscribers AND BindAbility bindings) lives outside the
// serialized state by design, so every restore path must rebuild it.
func decodeG(raw json.RawMessage) (core.G, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	g := &State{State: &ccg.State{}}
	if err := json.Unmarshal(raw, g); err != nil {
		return nil, err
	}
	wire(g)
	return g, nil
}

// wire registers the game's event subscribers on a fresh or restored
// State: the global enter-the-battlefield router, and a BindAbility for
// every Glowmoth already on a battlefield.
func wire(g *State) {
	g.Subscribe(ccg.MatchType("creature_entered"), func(s *ccg.State, ev ccg.Event) {
		e, ok := s.Get(ev.Source)
		if !ok {
			return
		}
		// Duskwisp: "when this enters, the opposing player loses 1
		// life." Staged, not pushed — the checkpoint flushes it.
		if e.DefID == "duskwisp" {
			s.StageTrigger(ccg.EffectFrom(s, ev.Source, "drain",
				map[string]any{"amount": 1, "only_opponent": true}))
		}
		if e.DefID == "glowmoth" {
			bindGlowmoth(s, ev.Source)
		}
	})
	for _, pid := range orderedKeys(g.Players) {
		z, ok := g.Zones[fieldZone(pid)]
		if !ok {
			continue
		}
		for _, id := range z.Members {
			if e, ok := g.Get(id); ok && e.DefID == "glowmoth" {
				bindGlowmoth(g.State, id)
			}
		}
	}
}

// bindGlowmoth wires "whenever ANOTHER creature enters, this card's
// controller gains 1 life" — scoped to the moth's battlefield life via
// BindAbility, which auto-unbinds when it leaves the zone.
func bindGlowmoth(s *ccg.State, moth ccg.EntityID) {
	owner := ""
	if e, ok := s.Get(moth); ok {
		owner = e.EffectiveController()
	}
	s.BindAbility(moth, []ccg.ZoneName{fieldZone(owner)},
		ccg.MatchType("creature_entered"),
		func(state *ccg.State, ev ccg.Event) {
			if ev.Source == moth {
				return
			}
			state.StageTrigger(ccg.EffectFrom(state, moth, "gain_life", map[string]any{"amount": 1}))
		})
}

// --- moves ------------------------------------------------------------

var (
	errWindowOpen       = errors.New("stackduel: priority window is open — respond or pass")
	errStackBusy        = errors.New("stackduel: stack is not empty")
	errNotInHand        = errors.New("stackduel: card is not in your hand")
	errNotInstant       = errors.New("stackduel: card is not castable in a response window")
	errNothingToCounter = errors.New("stackduel: no spell on the stack to counter")
)

// cast is the turn player's sorcery-speed cast: pay, put the spell on
// the stack, and open a priority window over APNAP order.
func cast(mc *core.MoveContext, args ...any) (core.G, error) {
	g := mc.G.(*State)
	if g.Priority.IsOpen() {
		return nil, errWindowOpen
	}
	if err := putOnStack(g, mc.PlayerID, args); err != nil {
		return nil, err
	}
	if err := g.Priority.OpenWindow(mc, apnap(mc.Ctx), respondStage); err != nil {
		return nil, err
	}
	return g, nil
}

// respond is an instant-speed cast by the priority holder. The actor
// retains priority (they may respond to their own spell).
func respond(mc *core.MoveContext, args ...any) (core.G, error) {
	g := mc.G.(*State)
	if err := putOnStack(g, mc.PlayerID, args, requireInstant); err != nil {
		return nil, err
	}
	if err := g.Priority.ActionTaken(mc, true); err != nil {
		return nil, err
	}
	return g, nil
}

// pass rotates priority; an unbroken all-pass round resolves exactly
// one stack object, flushes its triggers in APNAP order, and reopens
// the window while anything is left to resolve.
func pass(mc *core.MoveContext, _ ...any) (core.G, error) {
	g := mc.G.(*State)
	closed, err := g.Priority.Pass(mc)
	if err != nil {
		return nil, err
	}
	if !closed {
		return g, nil
	}
	if _, _, err := g.ResolveNext(ccg.HaltWhileOpen(&g.Priority, ccg.PickBack), resolvers); err != nil {
		return nil, err
	}
	order := apnap(mc.Ctx)
	g.FlushTriggers(func(effs []ccg.Effect) []ccg.Effect {
		return ccg.OrderByPlayer(effs, order)
	})
	if len(g.PendingEffects) > 0 {
		if err := g.Priority.OpenWindow(mc, order, respondStage); err != nil {
			return nil, err
		}
	}
	return g, nil
}

func endTurn(mc *core.MoveContext, _ ...any) (core.G, error) {
	g := mc.G.(*State)
	if g.Priority.IsOpen() {
		return nil, errWindowOpen
	}
	if len(g.PendingEffects) > 0 {
		return nil, errStackBusy
	}
	mc.Events.EndTurn()
	return g, nil
}

func turnBegin(mc *core.MoveContext) core.G {
	g := mc.G.(*State)
	// Hygiene: a window must be closed before EndTurn (endTurn enforces
	// it), but a forced boundary would leave stale protocol state.
	g.Priority.Reset()
	pid := mc.Ctx.CurrentPlayer
	for _, kind := range []string{"sun", "moon"} {
		pool(g, pid, kind).Set(g.State, manaPerTurn)
	}
	drawCards(g, pid, 1)
	return g
}

func endIf(mc *core.MoveContext) any {
	g := mc.G.(*State)
	var dead []string
	for _, pid := range mc.Ctx.PlayOrder {
		if g.Counters(g.Players[pid], lifeKind) <= 0 {
			dead = append(dead, pid)
		}
	}
	switch len(dead) {
	case 0:
		return nil
	case 1:
		return map[string]any{"winner": opponentOf(mc.Ctx, dead[0])}
	default:
		return map[string]any{"draw": true}
	}
}

// --- casting ----------------------------------------------------------

// castCheck is an extra legality predicate applied by a casting move.
type castCheck func(sp spec) error

func requireInstant(sp spec) error {
	if !sp.instant {
		return errNotInstant
	}
	return nil
}

// putOnStack validates and pays for a cast, moves the card entity to
// the stack zone, and pushes its pending effect (append = stack top
// under PickBack).
func putOnStack(g *State, pid string, args []any, checks ...castCheck) error {
	id, err := entityArg(args)
	if err != nil {
		return err
	}
	e, ok := g.Get(id)
	if !ok || e.Zone != handZone(pid) || e.Owner != pid {
		return errNotInHand
	}
	sp, ok := specs[e.DefID]
	if !ok {
		return fmt.Errorf("stackduel: unknown card def %q", e.DefID)
	}
	for _, check := range checks {
		if err := check(sp); err != nil {
			return err
		}
	}
	data := map[string]any{}
	for k, v := range sp.data {
		data[k] = v
	}
	if sp.kind == "counter" {
		if len(g.PendingEffects) == 0 {
			return errNothingToCounter
		}
		// Target the current top of the stack. A game with free
		// targeting would take this from args or a TargetRequest.
		data["target_effect"] = uint64(g.PendingEffects[len(g.PendingEffects)-1].ID)
	}
	// Pay before mutating zones — Basket is all-or-nothing, so a failed
	// payment leaves the cast entirely unapplied.
	basket := manaBasket(g, pid)
	if sp.generic > 0 {
		err = basket.PayWithGeneric(g.State, sp.cost, sp.generic, []string{"sun", "moon"})
	} else {
		err = basket.Pay(g.State, sp.cost)
	}
	if err != nil {
		return err
	}
	if err := g.MoveTo(id, stackZone); err != nil {
		return err
	}
	g.PushEffect(ccg.Effect{Source: id, Controller: pid, Kind: sp.kind, Data: data})
	return nil
}

// --- resolvers --------------------------------------------------------

// Resolvers operate purely on *ccg.State: life and mana are counters
// on the player entities, so no game wrapper is needed at resolve time.
var resolvers = ccg.ResolverTable{
	// damage: the controller's opponent loses `amount`.
	"damage": func(s *ccg.State, eff ccg.Effect) error {
		s.RemoveCounter(playerEntity(s, opponentInState(s, eff.Controller)), lifeKind, dataInt(eff, "amount"))
		return binSource(s, eff)
	},
	// gain_life: the controller gains `amount`.
	"gain_life": func(s *ccg.State, eff ccg.Effect) error {
		s.AddCounter(playerEntity(s, eff.Controller), lifeKind, dataInt(eff, "amount"))
		return binSource(s, eff)
	},
	// drain: every player — or only the controller's opponent when
	// only_opponent is set — loses `amount`.
	"drain": func(s *ccg.State, eff ccg.Effect) error {
		amount := dataInt(eff, "amount")
		for _, ent := range ccg.Query(s).HasType("player").Find() {
			e, _ := s.Get(ent)
			if eff.Data["only_opponent"] == true && e.Owner == eff.Controller {
				continue
			}
			s.RemoveCounter(ent, lifeKind, amount)
		}
		return binSource(s, eff)
	},
	// counter: remove the targeted pending effect and bin its card. A
	// missing target means it already resolved or was removed — the
	// counter fizzles silently.
	"counter": func(s *ccg.State, eff ccg.Effect) error {
		target := ccg.EffectID(dataUint(eff, "target_effect"))
		if countered, _, ok := s.FindEffect(target); ok {
			s.RemoveEffect(target)
			if e, exists := s.Get(countered.Source); exists && e.Zone == stackZone {
				_ = s.MoveTo(countered.Source, graveZone(e.Owner))
			}
		}
		return binSource(s, eff)
	},
	// summon: the creature leaves the stack for its controller's
	// battlefield and announces itself; triggers stage off the event.
	"summon": func(s *ccg.State, eff ccg.Effect) error {
		if err := s.MoveTo(eff.Source, fieldZone(eff.Controller)); err != nil {
			return err
		}
		s.Publish(ccg.Event{Type: "creature_entered", Source: eff.Source})
		return nil
	},
}

// binSource moves a resolved (or fizzled) spell card from the stack to
// its owner's graveyard. Effects whose source is not on the stack — a
// battlefield creature's trigger — leave their source untouched.
func binSource(s *ccg.State, eff ccg.Effect) error {
	if e, ok := s.Get(eff.Source); ok && e.Zone == stackZone {
		return s.MoveTo(eff.Source, graveZone(e.Owner))
	}
	return nil
}

// --- helpers ----------------------------------------------------------

func handZone(pid string) ccg.ZoneName  { return ccg.ZoneName("hand:" + pid) }
func deckZone(pid string) ccg.ZoneName  { return ccg.ZoneName("deck:" + pid) }
func fieldZone(pid string) ccg.ZoneName { return ccg.ZoneName("battlefield:" + pid) }
func graveZone(pid string) ccg.ZoneName { return ccg.ZoneName("graveyard:" + pid) }

func pool(g *State, pid, kind string) economy.Pool {
	return economy.Pool{Owner: g.Players[pid], Kind: kind, Cap: manaPerTurn}
}

func manaBasket(g *State, pid string) economy.Basket {
	return economy.Basket{Pools: map[string]economy.Pool{
		"sun":  pool(g, pid, "sun"),
		"moon": pool(g, pid, "moon"),
	}}
}

// Life reads a player's current life total.
func (g *State) LifeOf(pid string) int {
	return g.Counters(g.Players[pid], lifeKind)
}

func drawCards(g *State, pid string, n int) {
	ids, err := g.Draw(deckZone(pid), n)
	if err != nil {
		return // deck empty — this duel just stops drawing
	}
	for _, id := range ids {
		if err := g.Add(handZone(pid), id); err != nil {
			panic(fmt.Sprintf("stackduel: draw to hand: %v", err))
		}
	}
}

// playerEntity finds the player entity owned by pid. Query results are
// sorted by EntityID, so lookups are deterministic.
func playerEntity(s *ccg.State, pid string) ccg.EntityID {
	for _, ent := range ccg.Query(s).HasType("player").OwnedBy(pid).Find() {
		return ent
	}
	return 0
}

// opponentInState finds the other player's seat from the entity table —
// resolvers have no Ctx, but the player entities carry the seats.
func opponentInState(s *ccg.State, pid string) string {
	for _, ent := range ccg.Query(s).HasType("player").Find() {
		if e, ok := s.Get(ent); ok && e.Owner != pid {
			return e.Owner
		}
	}
	return ""
}

// apnap is the priority rotation: play order rotated so the turn
// player receives priority first.
func apnap(ctx core.Ctx) []string {
	order := ctx.PlayOrder
	for i, pid := range order {
		if pid == ctx.CurrentPlayer {
			return append(append([]string{}, order[i:]...), order[:i]...)
		}
	}
	return append([]string{}, order...)
}

func opponentOf(ctx core.Ctx, pid string) string {
	for _, other := range ctx.PlayOrder {
		if other != pid {
			return other
		}
	}
	return ""
}

// orderedKeys iterates map keys deterministically — never range a map
// for anything order-dependent in engine callbacks.
func orderedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}

// entityArg coerces a move argument to an EntityID. Live Go callers
// pass the typed value; transports and replays deliver JSON numbers.
func entityArg(args []any) (ccg.EntityID, error) {
	if len(args) == 0 {
		return 0, errors.New("stackduel: missing card argument")
	}
	switch v := args[0].(type) {
	case ccg.EntityID:
		return v, nil
	case uint64:
		return ccg.EntityID(v), nil
	case int:
		return ccg.EntityID(v), nil
	case float64:
		return ccg.EntityID(v), nil
	}
	return 0, fmt.Errorf("stackduel: bad card argument %T", args[0])
}

// dataInt / dataUint read numbers out of Effect.Data, tolerating the
// float64 that JSON round-trips produce.
func dataInt(eff ccg.Effect, key string) int {
	return int(dataUint(eff, key))
}

func dataUint(eff ccg.Effect, key string) uint64 {
	switch v := eff.Data[key].(type) {
	case int:
		return uint64(v)
	case uint64:
		return v
	case float64:
		return uint64(v)
	}
	return 0
}
