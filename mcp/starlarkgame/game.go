package starlarkgame

import (
	"context"
	"fmt"

	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/mcp/modulebridge"
)

// NewModuleStates mints a fresh live state for each module the spec
// declares in MODULES, keyed by module name. The result populates
// BridgeCtx.Modules so ctx.modules.<name>.<op>(...) bindings exist.
// Every code path that runs spec callbacks (live Setup, Validate,
// playtest) must call this — otherwise ctx.modules is silently absent.
func (s *Spec) NewModuleStates() map[string]any {
	mods := map[string]any{}
	for _, name := range s.Modules {
		if st := modulebridge.NewState(name); st != nil {
			mods[name] = st
		}
	}
	return mods
}

// BuildCoreGame synthesizes a *core.Game whose Setup / Moves / EndIf /
// PlayerView / Enumerate function fields are closures dispatching to
// the Spec's Starlark callables.
//
// The adapter intentionally ignores Spec advanced features (phases,
// stages, multi-action turns) because designed games don't expose them
// in v1.
func BuildCoreGame(s *Spec) *core.Game {
	g := &core.Game{
		Name:       s.Meta.Name,
		MinPlayers: s.Meta.MinPlayers,
		MaxPlayers: s.Meta.MaxPlayers,
		// Designed games end the turn per-move via Move.EndsTurn (default
		// true): the MoveFn closure below enqueues an EndTurn event when
		// the move that just ran is marked terminal. This lets specs opt
		// into multi-action turns (roll-then-build, draw-then-play) by
		// marking the non-terminal moves "ends_turn": False in MOVES.
		// We deliberately do NOT set MaxMoves here — that would force
		// the engine to auto-end after every move regardless of the
		// per-move flag.
	}

	g.Setup = func(ctx core.Ctx, _ any) core.G {
		// Instantiate a live state for each declared module before setup
		// runs, so setup code can call ctx.modules.<name>.<op>(...).
		mods := s.NewModuleStates()
		bc := NewWriteCtx(ctx.NumPlayers, "", mods)
		bc.AttachSeededRandom(ctxSeed(ctx))
		data, err := s.CallSetup(context.Background(), bc)
		if err != nil {
			// SetupFn has no error channel; encode failure as a state that
			// will immediately fail end_if.
			return &StarlarkG{Data: map[string]any{"__starlark_setup_error__": err.Error()}, Modules: mods}
		}
		sg := &StarlarkG{Data: data, Modules: mods}
		registerHooks(s, mods)
		return sg
	}

	g.EndIf = func(mc *core.MoveContext) any {
		sg, ok := asStarlarkG(mc.G)
		if !ok {
			return nil
		}
		bc := NewReadCtx(mc.Ctx.NumPlayers, "", sg.Modules)
		bc.AttachSeededRandom(ctxSeed(mc.Ctx))
		out, err := s.CallEndIf(context.Background(), bc, sg.Data)
		if err != nil {
			return nil
		}
		return out
	}

	if s.PlayerView != nil {
		g.PlayerView = func(gv core.G, ctx core.Ctx, playerID string) core.G {
			sg, ok := asStarlarkG(gv)
			if !ok {
				return gv
			}
			bc := NewReadCtx(ctx.NumPlayers, playerID, sg.Modules)
			out, err := s.CallPlayerView(context.Background(), bc, sg.Data, playerID)
			if err != nil {
				return gv
			}
			return &StarlarkG{Data: out, Modules: sg.Modules}
		}
	}

	g.Enumerate = func(gv core.G, ctx core.Ctx, playerID string) []core.EnumerateAction {
		sg, ok := asStarlarkG(gv)
		if !ok {
			return nil
		}
		bc := NewReadCtx(ctx.NumPlayers, playerID, sg.Modules)
		bc.AttachSeededRandom(ctxSeed(ctx))
		out, err := s.CallLegalMoves(context.Background(), bc, sg.Data)
		if err != nil {
			return nil
		}
		acts := make([]core.EnumerateAction, 0, len(out))
		for _, m := range out {
			name := legalMoveName(m)
			args, _ := m["args"].([]any)
			acts = append(acts, core.EnumerateAction{Move: name, Args: args})
		}
		return acts
	}

	g.Moves = buildMovesMap(s, s.Moves)

	if len(s.Stages) > 0 {
		stages := make(map[string]*core.StageConfig, len(s.Stages))
		for stageName, st := range s.Stages {
			stages[stageName] = &core.StageConfig{
				Moves: buildMovesMap(s, st.Moves),
				Next:  st.Next,
			}
		}
		g.Turn = &core.TurnConfig{Stages: stages}
	}

	if len(s.Phases) > 0 {
		g.Phases = make(map[string]*core.PhaseConfig, len(s.Phases))
		for phaseName, ph := range s.Phases {
			phaseName := phaseName
			ph := ph
			pc := &core.PhaseConfig{
				Moves: buildMovesMap(s, ph.Moves),
				Start: ph.Start,
			}
			if ph.EndIf != nil {
				pc.EndIf = func(mc *core.MoveContext) (bool, string) {
					sg, ok := asStarlarkG(mc.G)
					if !ok {
						return false, ""
					}
					bc := NewReadCtx(mc.Ctx.NumPlayers, mc.PlayerID, sg.Modules)
					bc.Phase = mc.Ctx.Phase
					bc.AttachSeededRandom(ctxSeed(mc.Ctx))
					next, err := s.CallPhaseEndIf(context.Background(), bc, sg.Data, phaseName)
					if err != nil || next == "" {
						return false, ""
					}
					return true, next
				}
			}
			g.Phases[phaseName] = pc
		}
	}

	return g
}

// buildMovesMap renders a moves table (top-level MOVES or a phase's
// scoped moves) into core.MoveFn closures. Each closure dispatches to
// the spec's apply function via CallMove and enqueues an EndTurn event
// when the move is marked terminal.
func buildMovesMap(s *Spec, src map[string]Move) map[string]any {
	out := make(map[string]any, len(src))
	for name, mv := range src {
		name := name
		mv := mv
		endsTurn := mv.EndsTurn
		out[name] = core.MoveFn(func(mc *core.MoveContext, args ...any) (core.G, error) {
			sg, ok := asStarlarkG(mc.G)
			if !ok {
				return nil, fmt.Errorf("starlarkgame: state is not *StarlarkG")
			}
			if err := validateArgs(mv, args, sg.Modules); err != nil {
				return nil, err
			}
			// On a serializing store the match reloads each move as a bare
			// map, so asStarlarkG rehydrates fresh module states with no
			// event subscriptions. Re-register the spec's HOOKS against
			// them. On the in-memory store mc.G is already *StarlarkG
			// (subs registered at Setup persist) — skip to avoid
			// double-subscribing.
			if _, wasLive := mc.G.(*StarlarkG); !wasLive {
				registerHooks(s, sg.Modules)
			}
			bc := NewWriteCtx(mc.Ctx.NumPlayers, mc.PlayerID, sg.Modules)
			bc.Phase = mc.Ctx.Phase
			bc.Events = mc.Events
			bc.Queue = mc.Queue
			bc.ResumingBlock = mc.ResumingBlock
			bc.AttachSeededRandom(ctxSeed(mc.Ctx))
			newData, err := s.CallMove(context.Background(), bc, name, sg.Data, args)
			if err != nil {
				return nil, err
			}
			if endsTurn && mc.Events != nil {
				mc.Events.EndTurn()
			}
			return &StarlarkG{Data: newData, Modules: sg.Modules}, nil
		})
	}
	return out
}

// ctxSeed returns a per-call seed derived from core.Ctx. Same Ctx fields →
// same seed, which ensures deterministic replay within a turn.
//
// core.Ctx has no dedicated seed field (it would require a core/ change,
// which is out of scope). Instead we mix Turn and NumPlayers with a small
// FNV-inspired multiplier. The result is stable for any given (turn,
// numPlayers) pair, which is the best determinism guarantee achievable
// from observable Ctx fields without touching core/.
//
// TODO: if core.Ctx ever gains a numeric seed field, replace this with:
//
//	return ctx.Seed
func ctxSeed(ctx core.Ctx) uint64 {
	// Mix turn and numPlayers into a stable uint64.
	// Uses the FNV-1a offset and prime to spread values across the range.
	const (
		offset64 uint64 = 14695981039346656037
		prime64  uint64 = 1099511628211
	)
	h := offset64
	h ^= uint64(ctx.Turn)
	h *= prime64
	h ^= uint64(ctx.NumPlayers)
	h *= prime64
	return h
}
