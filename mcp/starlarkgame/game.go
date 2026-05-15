package starlarkgame

import (
	"context"
	"fmt"

	"github.com/tjcran/boardgame-go/core"
)

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
	}

	g.Setup = func(ctx core.Ctx, _ any) core.G {
		bc := &BridgeCtx{NumPlayers: ctx.NumPlayers}
		bc.AttachSeededRandom(ctxSeed(ctx))
		state, err := s.CallSetup(context.Background(), bc)
		if err != nil {
			// SetupFn has no error channel; encode failure as a state that
			// will immediately fail end_if.
			return map[string]any{"__starlark_setup_error__": err.Error()}
		}
		return state
	}

	g.EndIf = func(mc *core.MoveContext) any {
		bc := &BridgeCtx{NumPlayers: mc.Ctx.NumPlayers}
		bc.AttachSeededRandom(ctxSeed(mc.Ctx))
		state, ok := mc.G.(map[string]any)
		if !ok {
			return nil
		}
		out, err := s.CallEndIf(context.Background(), bc, state)
		if err != nil {
			return nil
		}
		return out
	}

	if s.PlayerView != nil {
		g.PlayerView = func(gv core.G, ctx core.Ctx, playerID string) core.G {
			bc := &BridgeCtx{NumPlayers: ctx.NumPlayers, PlayerID: playerID}
			state, ok := gv.(map[string]any)
			if !ok {
				return gv
			}
			out, err := s.CallPlayerView(context.Background(), bc, state, playerID)
			if err != nil {
				return gv
			}
			return out
		}
	}

	g.Enumerate = func(gv core.G, ctx core.Ctx, playerID string) []core.EnumerateAction {
		bc := &BridgeCtx{NumPlayers: ctx.NumPlayers, PlayerID: playerID}
		bc.AttachSeededRandom(ctxSeed(ctx))
		state, ok := gv.(map[string]any)
		if !ok {
			return nil
		}
		out, err := s.CallLegalMoves(context.Background(), bc, state)
		if err != nil {
			return nil
		}
		acts := make([]core.EnumerateAction, 0, len(out))
		for _, m := range out {
			name, _ := m["name"].(string)
			args, _ := m["args"].([]any)
			acts = append(acts, core.EnumerateAction{Move: name, Args: args})
		}
		return acts
	}

	moves := make(map[string]any, len(s.Moves))
	for name, mv := range s.Moves {
		name := name
		_ = mv // captured in name; apply is dispatched via CallMove
		moves[name] = core.MoveFn(func(mc *core.MoveContext, args ...any) (core.G, error) {
			bc := &BridgeCtx{
				NumPlayers: mc.Ctx.NumPlayers,
				PlayerID:   mc.PlayerID,
			}
			bc.AttachSeededRandom(ctxSeed(mc.Ctx))
			state, ok := mc.G.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("starlarkgame: state is not map[string]any")
			}
			newState, err := s.CallMove(context.Background(), bc, name, state, args)
			if err != nil {
				return nil, err
			}
			return newState, nil
		})
	}
	g.Moves = moves
	return g
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
