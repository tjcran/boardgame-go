package core

// Plugin is the extension point for adding cross-cutting behavior to the
// engine (state under a private key, an API exposed to moves, per-move
// wrappers, server-only behaviour, per-seat state redaction).
//
// Mirrors boardgame.io's plugin object — see plugins.md in the BGIO docs.
//
// Real plugin types will land alongside the plugin system implementation
// (task #17). This skeleton is just enough for the reducer to compile
// while we plumb plugin support in.
type Plugin interface {
	// Name is the unique key under which this plugin's API is exposed in
	// MoveContext.Plugins and its private data is stored in State.Plugins.
	Name() string
}

// PluginSetup describes a plugin that initialises private state at match
// start.
type PluginSetup interface {
	Plugin
	Setup(g G, ctx Ctx, game *Game) any
}

// PluginAPI describes a plugin that exposes an API into MoveContext.
type PluginAPI interface {
	Plugin
	API(data any, g G, ctx Ctx, playerID string, game *Game) any
}

// PluginFlush describes a plugin that wants to persist mutated data at the
// end of each move.
type PluginFlush interface {
	Plugin
	Flush(data, api any, g G, ctx Ctx, game *Game) any
}

// PluginPlayerView describes a plugin that filters its private data per
// seat before the state is pushed to a client.
type PluginPlayerView interface {
	Plugin
	PlayerView(data any, g G, ctx Ctx, playerID string, game *Game) any
}
