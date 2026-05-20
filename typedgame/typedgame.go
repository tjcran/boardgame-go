// Package typedgame is a thin generics-based wrapper over the untyped
// core.Game. It lets game authors declare their G with a real Go type
// and write moves/hooks without runtime type assertions.
//
// Internally a typed Game is converted to a *core.Game by Build(); the
// engine remains untyped (so plugins, transports and storage don't need
// generic instantiation).
//
//	type State struct{ Score int }
//
//	var MyGame = typedgame.Game[*State, any]{
//	    Name:  "counter",
//	    Setup: func(_ core.Ctx, _ any) *State { return &State{} },
//	    Moves: typedgame.Moves[*State]{
//	        "inc": typedgame.MoveFn[*State](func(mc *typedgame.Context[*State], _ ...any) (*State, error) {
//	            return &State{Score: mc.G.Score + 1}, nil
//	        }),
//	    },
//	}
//
// For games with typed setup data, parameterise the second slot too:
//
//	type Setup struct{ Seed int64 }
//	var MyGame = typedgame.Game[*State, *Setup]{
//	    Setup: func(_ core.Ctx, sd *Setup) *State { return &State{Seed: sd.Seed} },
//	    // ...
//	}
//
//	m.MustRegister(MyGame.Build())
//
// BGIO can't offer this — TypeScript types help in the IDE but the
// framework still uses an untyped object at runtime. Here the compiler
// enforces the contract: a move that misnames a field on G doesn't build.
package typedgame

import (
	"context"

	"github.com/tjcran/boardgame-go/core"
)

// Context is the typed analogue of core.MoveContext. G is typed S; the
// rest of the fields are the underlying core types because they don't
// benefit from generics.
type Context[S any] struct {
	G        S
	Ctx      core.Ctx
	PlayerID string
	Events   *core.Events
	Random   *core.Random
	Context  context.Context
	Plugins  map[string]any
}

// Plugin is a tiny passthrough for callers who want to type-assert into
// a known plugin API.
func (c *Context[S]) Plugin(name string) any {
	if c.Plugins == nil {
		return nil
	}
	return c.Plugins[name]
}

// MoveFn is the typed move signature.
type MoveFn[S any] func(mc *Context[S], args ...any) (S, error)

// HookFn is the typed hook signature for OnBegin/OnEnd/OnMove.
type HookFn[S any] func(mc *Context[S]) S

// SetupFn is the typed initial-state builder. SD is the (typed) setup
// data payload — pass `any` if your game doesn't take typed setup data.
type SetupFn[S, SD any] func(ctx core.Ctx, setupData SD) S

// ValidateSetupDataFn is the typed setup-data validator. Returns an
// empty string when valid, or an error message when invalid. SD is
// the typed payload.
type ValidateSetupDataFn[SD any] func(setupData SD, numPlayers int) string

// EndIfFn is the typed game-end predicate.
type EndIfFn[S any] func(mc *Context[S]) any

// PlayerViewFn is the typed per-seat redactor.
type PlayerViewFn[S any] func(g S, ctx core.Ctx, playerID string) S

// PhaseEndIfFn is the typed phase-end predicate. (end, nextPhase).
type PhaseEndIfFn[S any] func(mc *Context[S]) (bool, string)

// TurnEndIfFn is the typed turn-end predicate. (end, nextPlayer).
type TurnEndIfFn[S any] func(mc *Context[S]) (bool, string)

// Move is the long-form typed move definition. Same flags as the
// underlying core.Move.
type Move[S any] struct {
	Move               MoveFn[S]
	Undoable           any // bool or func(*Context[S]) bool
	Redact             any // bool or func(*Context[S]) bool
	ServerOnly         bool
	NoLimit            bool
	IgnoreStaleStateID bool
}

// Moves is the typed move table. Values may be a MoveFn[S] or a Move[S]
// — both forms are normalised at Build() time.
type Moves[S any] map[string]any

// StageConfig is the typed stage.
type StageConfig[S any] struct {
	Moves Moves[S]
	Next  string
}

// TurnConfig is the typed turn config. Order and ActivePlayers reuse
// core types because they don't depend on S.
type TurnConfig[S any] struct {
	Order         core.TurnOrder
	OnBegin       HookFn[S]
	OnEnd         HookFn[S]
	OnMove        HookFn[S]
	EndIf         TurnEndIfFn[S]
	MinMoves      int
	MaxMoves      int
	ActivePlayers *core.ActivePlayersConfig
	Stages        map[string]*StageConfig[S]
}

// PhaseConfig is the typed phase config. Next mirrors core's loose typing
// (string or func(*Context[S]) string).
type PhaseConfig[S any] struct {
	OnBegin HookFn[S]
	OnEnd   HookFn[S]
	EndIf   PhaseEndIfFn[S]
	Moves   Moves[S]
	Turn    *TurnConfig[S]
	Start   bool
	Next    any // string or func(*Context[S]) string
}

// Game is the typed game definition. S is the state type; SD is the
// typed setup-data payload (use `any` if your game doesn't need typed
// setup).
type Game[S, SD any] struct {
	Name              string
	MinPlayers        int
	MaxPlayers        int
	Setup             SetupFn[S, SD]
	ValidateSetupData ValidateSetupDataFn[SD]
	Seed              any
	DisableUndo       bool
	DeltaState        bool
	Plugins           []core.Plugin
	Moves             Moves[S]
	Turn              *TurnConfig[S]
	Phases            map[string]*PhaseConfig[S]
	EndIf             EndIfFn[S]
	OnEnd             HookFn[S]
	PlayerView        PlayerViewFn[S]
}

// Build compiles a typed Game into the engine's untyped *core.Game. The
// returned object is safe to register with match.Manager.
func (g Game[S, SD]) Build() *core.Game {
	out := &core.Game{
		Name:        g.Name,
		MinPlayers:  g.MinPlayers,
		MaxPlayers:  g.MaxPlayers,
		Seed:        g.Seed,
		DisableUndo: g.DisableUndo,
		DeltaState:  g.DeltaState,
		Plugins:     g.Plugins,
	}
	if g.Setup != nil {
		setup := g.Setup
		out.Setup = func(ctx core.Ctx, setupData any) core.G {
			var sd SD
			if setupData != nil {
				sd = setupData.(SD)
			}
			return setup(ctx, sd)
		}
	}
	if g.ValidateSetupData != nil {
		validate := g.ValidateSetupData
		out.ValidateSetupData = func(setupData any, numPlayers int) string {
			var sd SD
			if setupData != nil {
				sd = setupData.(SD)
			}
			return validate(sd, numPlayers)
		}
	}
	if g.EndIf != nil {
		ef := g.EndIf
		out.EndIf = func(mc *core.MoveContext) any { return ef(toTyped[S](mc)) }
	}
	if g.OnEnd != nil {
		oe := g.OnEnd
		out.OnEnd = func(mc *core.MoveContext) core.G { return oe(toTyped[S](mc)) }
	}
	if g.PlayerView != nil {
		pv := g.PlayerView
		out.PlayerView = func(gg core.G, ctx core.Ctx, pid string) core.G {
			return pv(gg.(S), ctx, pid)
		}
	}
	out.Moves = convertMoves[S](g.Moves)
	if g.Turn != nil {
		out.Turn = convertTurn[S](g.Turn)
	}
	if len(g.Phases) > 0 {
		out.Phases = map[string]*core.PhaseConfig{}
		for name, p := range g.Phases {
			out.Phases[name] = convertPhase[S](p)
		}
	}
	return out
}

// toTyped wraps an untyped core.MoveContext into a typed Context[S].
// Allocated per call — the wrapper is cheap (one struct value) and lets
// the typed move see G as S without an assertion on every field access.
func toTyped[S any](mc *core.MoveContext) *Context[S] {
	return &Context[S]{
		G:        mc.G.(S),
		Ctx:      mc.Ctx,
		PlayerID: mc.PlayerID,
		Events:   mc.Events,
		Random:   mc.Random,
		Context:  mc.Context,
		Plugins:  mc.Plugins,
	}
}

// convertMoves normalises a typed Moves table into the engine's
// `map[string]any` (values: bare core.MoveFn or core.Move).
func convertMoves[S any](in Moves[S]) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for name, raw := range in {
		out[name] = convertMoveValue[S](raw)
	}
	return out
}

func convertMoveValue[S any](raw any) any {
	switch m := raw.(type) {
	case MoveFn[S]:
		return wrapMoveFn[S](m)
	case Move[S]:
		return core.Move{
			Move:               wrapMoveFn[S](m.Move),
			Undoable:           wrapUndoable[S](m.Undoable),
			Redact:             wrapRedact[S](m.Redact),
			ServerOnly:         m.ServerOnly,
			NoLimit:            m.NoLimit,
			IgnoreStaleStateID: m.IgnoreStaleStateID,
		}
	default:
		// Untyped move passed through (e.g. for a built-in plugin's
		// helper). The engine will type-check at Apply time.
		return raw
	}
}

func wrapMoveFn[S any](fn MoveFn[S]) core.MoveFn {
	return func(mc *core.MoveContext, args ...any) (core.G, error) {
		return fn(toTyped[S](mc), args...)
	}
}

// wrapUndoable maps a typed Undoable spec (bool or func(*Context[S]) bool)
// into the untyped form core.Move.IsUndoable understands.
func wrapUndoable[S any](spec any) any {
	switch v := spec.(type) {
	case nil:
		return nil
	case bool:
		return v
	case func(*Context[S]) bool:
		return func(mc *core.MoveContext) bool { return v(toTyped[S](mc)) }
	default:
		return spec
	}
}

func wrapRedact[S any](spec any) any {
	switch v := spec.(type) {
	case nil:
		return nil
	case bool:
		return v
	case func(*Context[S]) bool:
		return func(mc *core.MoveContext) bool { return v(toTyped[S](mc)) }
	default:
		return spec
	}
}

func convertTurn[S any](t *TurnConfig[S]) *core.TurnConfig {
	out := &core.TurnConfig{
		Order:         t.Order,
		MinMoves:      t.MinMoves,
		MaxMoves:      t.MaxMoves,
		ActivePlayers: t.ActivePlayers,
	}
	if t.OnBegin != nil {
		fn := t.OnBegin
		out.OnBegin = func(mc *core.MoveContext) core.G { return fn(toTyped[S](mc)) }
	}
	if t.OnEnd != nil {
		fn := t.OnEnd
		out.OnEnd = func(mc *core.MoveContext) core.G { return fn(toTyped[S](mc)) }
	}
	if t.OnMove != nil {
		fn := t.OnMove
		out.OnMove = func(mc *core.MoveContext) core.G { return fn(toTyped[S](mc)) }
	}
	if t.EndIf != nil {
		fn := t.EndIf
		out.EndIf = func(mc *core.MoveContext) (bool, string) { return fn(toTyped[S](mc)) }
	}
	if len(t.Stages) > 0 {
		out.Stages = map[string]*core.StageConfig{}
		for name, st := range t.Stages {
			out.Stages[name] = &core.StageConfig{
				Moves: convertMoves[S](st.Moves),
				Next:  st.Next,
			}
		}
	}
	return out
}

func convertPhase[S any](p *PhaseConfig[S]) *core.PhaseConfig {
	out := &core.PhaseConfig{
		Moves: convertMoves[S](p.Moves),
		Start: p.Start,
	}
	if p.OnBegin != nil {
		fn := p.OnBegin
		out.OnBegin = func(mc *core.MoveContext) core.G { return fn(toTyped[S](mc)) }
	}
	if p.OnEnd != nil {
		fn := p.OnEnd
		out.OnEnd = func(mc *core.MoveContext) core.G { return fn(toTyped[S](mc)) }
	}
	if p.EndIf != nil {
		fn := p.EndIf
		out.EndIf = func(mc *core.MoveContext) (bool, string) { return fn(toTyped[S](mc)) }
	}
	if p.Turn != nil {
		out.Turn = convertTurn[S](p.Turn)
	}
	// Next: string passes through; func(*Context[S]) string is wrapped.
	switch n := p.Next.(type) {
	case nil:
		out.Next = nil
	case string:
		out.Next = n
	case func(*Context[S]) string:
		out.Next = func(mc *core.MoveContext) string { return n(toTyped[S](mc)) }
	default:
		out.Next = n
	}
	return out
}
