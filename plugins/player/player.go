// Package player implements BGIO's PluginPlayer: per-player state stored in
// a dedicated plugin slot, with ergonomic Get/Set helpers exposed to moves
// as `mc.Plugins["player"]` (or its strongly-typed wrapper).
//
// In BGIO the API looks like:
//
//	ctx.player.get()           // current player's record
//	ctx.player.set(record)
//	ctx.player.opponent.get()  // 2-player only
//	ctx.player.opponent.set(record)
//
// In Go that's:
//
//	api := mc.Plugins[player.PluginName].(*player.API)
//	rec := api.Get()
//	api.Set(rec)
//	op := api.Opponent.Get()
//	api.Opponent.Set(op)
//
// Setup builds per-player records via a user-supplied function, just like
// the JS plugin's `setup` option.
package player

import (
	"github.com/tjcran/boardgame-go/core"
)

// PluginName is the well-known key used to look the plugin up in
// MoveContext.Plugins and State.Plugins.
const PluginName = "player"

// Options carries the per-player setup and (optional) playerView functions
// passed to New. PlayerSetup is required; PlayerView falls back to "hide
// every record except mine" when nil.
type Options struct {
	// PlayerSetup builds the initial record for a given playerID. Called
	// once per seat at match start.
	PlayerSetup func(playerID string) any

	// PlayerView optionally redacts the players map before pushing state
	// to a specific seat. By default the plugin keeps only the requested
	// player's record (spectators get an empty map).
	PlayerView func(players map[string]any, playerID string) map[string]any
}

// Plugin is the plugin instance. Use New(Options{...}).
type Plugin struct {
	opts Options
}

// New constructs a configured PluginPlayer.
func New(opts Options) *Plugin {
	if opts.PlayerView == nil {
		opts.PlayerView = defaultPlayerView
	}
	return &Plugin{opts: opts}
}

// data is the persisted private payload: a map of playerID → user record.
type data struct {
	Players map[string]any `json:"players"`
}

// API is the value exposed to moves via mc.Plugins[PluginName].
type API struct {
	playerID string
	data     *data

	// Opponent is convenience access to the only other seat in a 2-player
	// match. Nil for non-2-player games.
	Opponent *API
}

// Get returns the current player's record.
func (a *API) Get() any { return a.data.Players[a.playerID] }

// Set replaces the current player's record.
func (a *API) Set(record any) { a.data.Players[a.playerID] = record }

// Name implements core.Plugin.
func (p *Plugin) Name() string { return PluginName }

// Setup builds the per-player records using the configured PlayerSetup.
// Order matches ctx.PlayOrder.
func (p *Plugin) Setup(_ core.G, ctx core.Ctx, _ *core.Game) any {
	d := &data{Players: map[string]any{}}
	if p.opts.PlayerSetup == nil {
		return d
	}
	for _, pid := range ctx.PlayOrder {
		d.Players[pid] = p.opts.PlayerSetup(pid)
	}
	return d
}

// API returns the per-call API for a specific seat.
func (p *Plugin) API(raw any, _ core.G, ctx core.Ctx, playerID string, _ *core.Game) any {
	d := raw.(*data)
	api := &API{playerID: playerID, data: d}
	if ctx.NumPlayers == 2 && playerID != "" {
		// Find the other seat.
		for _, pid := range ctx.PlayOrder {
			if pid != playerID {
				api.Opponent = &API{playerID: pid, data: d}
				break
			}
		}
	}
	return api
}

// Flush is a no-op: the API mutates the shared data pointer directly.
func (p *Plugin) Flush(d, _ any, _ core.G, _ core.Ctx, _ *core.Game) any {
	return d
}

// PlayerView filters per-seat data per the configured PlayerView. Defaults
// to "only your own record".
func (p *Plugin) PlayerView(raw any, _ core.G, _ core.Ctx, playerID string, _ *core.Game) any {
	d := raw.(*data)
	return &data{Players: p.opts.PlayerView(d.Players, playerID)}
}

func defaultPlayerView(players map[string]any, playerID string) map[string]any {
	out := map[string]any{}
	if playerID != "" {
		if v, ok := players[playerID]; ok {
			out[playerID] = v
		}
	}
	return out
}
