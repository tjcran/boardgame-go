package core

import "encoding/json"

// Plugin is the extension point for adding cross-cutting behavior to the
// engine. Implementations can store private state, expose APIs to moves,
// hide state from clients, and reject invalid actions.
//
// A real Plugin will typically implement several of the optional sub-
// interfaces below. The engine type-asserts at call sites and silently
// skips any method a given plugin doesn't implement.
//
// Mirrors boardgame.io's plugin object — see plugins.md in the BGIO docs.
type Plugin interface {
	// Name is the unique key under which this plugin's API is exposed in
	// MoveContext.Plugins and its private data is stored in State.Plugins.
	Name() string
}

// PluginSetup initialises private state at match start. Equivalent to
// BGIO's `plugin.setup`.
type PluginSetup interface {
	Plugin
	Setup(g G, ctx Ctx, game *Game) any
}

// PluginDecode rehydrates a plugin's private data after a persistence
// round-trip. Storage decodes State.Plugins values as generic JSON
// (map[string]any); plugins whose API hooks type-assert a concrete Go
// type implement Decode so the match manager can restore it (see
// Manager.loadMigrated — the plugin analogue of Game.DecodeG).
type PluginDecode interface {
	Plugin
	Decode(raw json.RawMessage) (any, error)
}

// PluginAPI exposes an API into MoveContext.Plugins at the start of each
// move. Equivalent to BGIO's `plugin.api`.
type PluginAPI interface {
	Plugin
	API(data any, g G, ctx Ctx, playerID string, game *Game) any
}

// PluginFlush persists mutated data at the end of each move (BGIO's
// `plugin.flush`). The api parameter is whatever the plugin's API method
// returned at the start of the move — it may have been mutated by the move.
type PluginFlush interface {
	Plugin
	Flush(data, api any, g G, ctx Ctx, game *Game) any
}

// PluginInvalid checks data validity after a move. Returning a non-nil
// error rejects the move and rewinds the state. Equivalent to BGIO's
// `plugin.isInvalid`.
type PluginInvalid interface {
	Plugin
	IsInvalid(data any, g G, ctx Ctx, game *Game) error
}

// PluginPlayerView filters private data per seat before pushing state to a
// client. Equivalent to BGIO's `plugin.playerView`.
type PluginPlayerView interface {
	Plugin
	PlayerView(data any, g G, ctx Ctx, playerID string, game *Game) any
}

// GameMethod identifies which engine entry point a plugin's FnWrap is
// wrapping. Mirrors BGIO's GameMethod enum.
type GameMethod int

const (
	GameMethodMove GameMethod = iota
	GameMethodTurnOnBegin
	GameMethodTurnOnEnd
	GameMethodTurnOnMove
	GameMethodPhaseOnBegin
	GameMethodPhaseOnEnd
	GameMethodGameOnEnd
	GameMethodStageOnBegin
	GameMethodStageOnEnd
)

// PluginFnWrapMove lets a plugin wrap MoveFns at registration time. The
// returned function is called instead of the original; it may inspect or
// mutate G before/after calling through. Mirrors BGIO's `fnWrap` for moves.
type PluginFnWrapMove interface {
	Plugin
	WrapMove(inner MoveFn) MoveFn
}

// PluginFnWrapHook is the hook-flavoured equivalent of PluginFnWrapMove.
// kind tells the plugin which kind of hook is being wrapped so it can
// behave differently for, say, TurnOnEnd vs. PhaseOnBegin.
type PluginFnWrapHook interface {
	Plugin
	WrapHook(inner HookFn, kind GameMethod) HookFn
}

// applyFnWrapMove threads every MoveFn through every PluginFnWrapMove. The
// engine calls this once at move-resolution time so plugins' wrappers
// compose left-to-right (matching BGIO).
func applyFnWrapMove(game *Game, fn MoveFn) MoveFn {
	for _, p := range game.Plugins {
		if w, ok := p.(PluginFnWrapMove); ok {
			fn = w.WrapMove(fn)
		}
	}
	return fn
}

// applyFnWrapHook threads a HookFn through every PluginFnWrapHook.
func applyFnWrapHook(game *Game, fn HookFn, kind GameMethod) HookFn {
	for _, p := range game.Plugins {
		if w, ok := p.(PluginFnWrapHook); ok {
			fn = w.WrapHook(fn, kind)
		}
	}
	return fn
}

// runPluginSetup initialises every plugin's private data and stores it on
// the state. Called from NewMatch.
func runPluginSetup(game *Game, state State) State {
	if len(game.Plugins) == 0 {
		return state
	}
	if state.Plugins == nil {
		state.Plugins = map[string]any{}
	}
	for _, p := range game.Plugins {
		if ps, ok := p.(PluginSetup); ok {
			state.Plugins[p.Name()] = ps.Setup(state.G, state.Ctx, game)
		}
	}
	return state
}

// buildPluginAPIs constructs the API objects exposed to a move. Returns the
// map and a per-plugin reference to the plugin data so Flush can persist
// updates.
func buildPluginAPIs(game *Game, state State, playerID string) map[string]any {
	if len(game.Plugins) == 0 {
		return nil
	}
	out := map[string]any{}
	for _, p := range game.Plugins {
		if pa, ok := p.(PluginAPI); ok {
			data := pluginDataFor(state, p.Name())
			out[p.Name()] = pa.API(data, state.G, state.Ctx, playerID, game)
		}
	}
	return out
}

// flushPlugins runs each plugin's Flush, capturing any data mutations into
// State.Plugins.
func flushPlugins(game *Game, state State, mc *MoveContext) State {
	if len(game.Plugins) == 0 {
		return state
	}
	if state.Plugins == nil {
		state.Plugins = map[string]any{}
	}
	for _, p := range game.Plugins {
		name := p.Name()
		api, _ := mc.Plugins[name]
		data := pluginDataFor(state, name)
		if pf, ok := p.(PluginFlush); ok {
			state.Plugins[name] = pf.Flush(data, api, mc.G, mc.Ctx, game)
		} else if api != nil {
			// No Flush implemented: persist the API object itself so plugin
			// authors who treat the API as a mutable view still see writes
			// survive. This matches the path BGIO uses when a plugin doesn't
			// supply flush — see plugins.md ("Return an updated version of
			// data that is persisted").
			state.Plugins[name] = api
		}
	}
	return state
}

// validatePlugins runs each plugin's IsInvalid; returns the first error
// produced. Called after the move (and post-flush) to give plugins a chance
// to reject the action.
func validatePlugins(game *Game, state State) error {
	for _, p := range game.Plugins {
		if pi, ok := p.(PluginInvalid); ok {
			if err := pi.IsInvalid(pluginDataFor(state, p.Name()),
				state.G, state.Ctx, game); err != nil {
				return err
			}
		}
	}
	return nil
}

// pluginDataFor returns the persisted private state for the named plugin,
// or nil if no entry exists yet.
func pluginDataFor(state State, name string) any {
	if state.Plugins == nil {
		return nil
	}
	return state.Plugins[name]
}

// redactPluginData runs PlayerView for every plugin that implements it,
// returning a new Plugins map suitable for sending to the given seat
// (playerID="" for spectators). Plugins without a PlayerView are passed
// through unchanged.
func redactPluginData(game *Game, state State, playerID string) map[string]any {
	if len(game.Plugins) == 0 || state.Plugins == nil {
		return state.Plugins
	}
	out := make(map[string]any, len(state.Plugins))
	for _, p := range game.Plugins {
		name := p.Name()
		data := state.Plugins[name]
		if pv, ok := p.(PluginPlayerView); ok {
			data = pv.PlayerView(data, state.G, state.Ctx, playerID, game)
		}
		if data != nil {
			out[name] = data
		}
	}
	return out
}
