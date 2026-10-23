package core

import (
	"context"
	"encoding/json"
)

// State is the complete authoritative state of a match. The transport sends
// (a redacted view of) this object to clients.
type State struct {
	// G is the user-defined game state.
	G G `json:"G"`

	// rawG is the raw JSON bytes that decoded into G on the last
	// UnmarshalJSON. The match manager passes it to Game.DecodeG to
	// reconstruct the concrete Go type (issue #80). Not persisted — the
	// default Marshal path round-trips G itself.
	rawG json.RawMessage `json:"-"`

	// rawPlugins mirrors rawG for each plugin's private data blob —
	// consumed by the manager's PluginDecode rehydration pass.
	rawPlugins map[string]json.RawMessage `json:"-"`

	// Ctx is the engine-managed metadata.
	Ctx Ctx `json:"ctx"`

	// StateID is a monotonic counter incremented on every accepted move.
	// Clients echo back the StateID they last saw so the server can detect
	// stale moves and reject them (unless Move.IgnoreStaleStateID = true).
	// Mirrors BGIO's `_stateID`.
	StateID int `json:"_stateID"`

	// Plugins is per-plugin private data, keyed by Plugin.Name. Hidden from
	// JSON when empty.
	Plugins map[string]any `json:"plugins,omitempty"`

	// Log is the ordered list of applied moves (and event transitions).
	// Used for undo/redo and replay. Reset at turn boundaries — entries
	// older than the current turn are pruned, matching BGIO's
	// per-turn undo scope.
	Log []LogEntry `json:"log,omitempty"`

	// Undone holds entries popped by Undo so Redo can replay them. Cleared
	// when a fresh move overwrites the redo stack (parity with BGIO).
	Undone []LogEntry `json:"_undone,omitempty"`

	// TurnSnapshots holds the State at the start of each pending move in
	// the current turn. Undo pops the latest snapshot and reinstates it.
	// Snapshots are private — never sent to clients.
	TurnSnapshots []State `json:"-"`

	// Queue is the pending FIFO of server-driven actions waiting for the
	// reducer to drain. Populated by moves and hooks calling
	// mc.Queue.Push; drained by the engine after each external move's
	// event drain (and after the hooks NewMatch and ForceEndTurn run).
	// Persisted across pauses (when Blocks is non-empty).
	Queue []QueuedAction `json:"queue,omitempty"`

	// Blocks pauses the cascade. The drain stops on the first block and
	// state is persisted. The next move whose ResumeTag + PlayerID match
	// removes a block; if all are gone the drain resumes.
	Blocks []BlockSpec `json:"blocks,omitempty"`

	// activeStack supports BGIO's `revert: true` on ActivePlayersConfig.
	// When SetActivePlayers is called with Revert=true, the previous
	// ActivePlayers map is pushed here; when the new set drains the engine
	// pops it back.
	ActiveStack []activeFrame `json:"_activeStack,omitempty"`

	// PendingNext is BGIO's "use this config after current active set
	// drains" support (the `next:` option on setActivePlayers).
	PendingNext *ActivePlayersConfig `json:"_pendingNext,omitempty"`

	// MoveCounts tracks per-active-player counting moves for MinMoves /
	// MaxMoves at the stage level.
	MoveCounts map[string]int `json:"_moveCounts,omitempty"`

	// StageMin/Max overrides per active player. Nil maps mean "no override
	// for this player".
	StageMinMoves map[string]int `json:"_stageMin,omitempty"`
	StageMaxMoves map[string]int `json:"_stageMax,omitempty"`
}

// activeFrame snapshots the active-players bookkeeping for Revert support.
type activeFrame struct {
	ActivePlayers map[string]string `json:"a"`
	MoveCounts    map[string]int    `json:"c"`
	StageMin      map[string]int    `json:"mi"`
	StageMax      map[string]int    `json:"ma"`
}

// NewMatch builds the starting State for a fresh match. setupData is passed
// through to Game.Setup; pass nil if the game doesn't use one. The match
// gets no secret seed (Ctx.Seed = 0) — use NewMatchSeeded when one is
// available; match.Manager does so for every match it creates.
func NewMatch(game *Game, numPlayers int, setupData any) State {
	return NewMatchSeeded(game, numPlayers, setupData, 0)
}

// NewMatchE is NewMatch that also reports a failed setup cascade; see
// NewMatchSeededE.
func NewMatchE(game *Game, numPlayers int, setupData any) (State, error) {
	return NewMatchSeededE(game, numPlayers, setupData, 0)
}

// NewMatchSeeded is NewMatch with a per-match secret seed installed in
// Ctx before Game.Setup runs, so setup-time randomness can already
// derive from it. Replays of a seeded match must use ReplaySeeded with
// the same value or RNG-dependent games diverge.
//
// The starting phase's OnBegin and the first turn's OnBegin run here with
// the same MoveContext a move's hooks get, Queue included. A block they
// raise is in the returned State.Blocks, so a match can open on a prompt
// (a mulligan, say). Moves they push drain before NewMatch returns,
// exactly as Apply drains a move's cascade, stopping at the first block
// or at game over; their log entries carry Parent -1, since no move
// started them.
//
// NewMatchSeeded has no error to return and no earlier state to roll back
// to. If a pushed move fails, or the drain exceeds MaxDrainDepth, the
// cascade is rolled back to where it started and its moves stay in
// State.Queue. Nothing is dropped, but the match cannot recover on its
// own: the next Apply runs those moves after its own move and, if one
// still fails, returns that error and refuses the move, as it does for
// any failing drain step — whatever the move, an IgnoreBlocks concede
// included. Callers that create matches for players should use
// NewMatchSeededE and refuse to create the match instead.
func NewMatchSeeded(game *Game, numPlayers int, setupData any, seed uint64) State {
	st, _ := NewMatchSeededE(game, numPlayers, setupData, seed)
	return st
}

// NewMatchSeededE is NewMatchSeeded that also returns the error of a
// failed setup cascade (the failing drain step's error, or
// ErrDrainOverflow). The State it returns alongside the error is the one
// NewMatchSeeded would return; a caller creating a match for players
// should discard it and report the error, since every move on it would
// fail. match.Manager does so.
func NewMatchSeededE(game *Game, numPlayers int, setupData any, seed uint64) (State, error) {
	n := game.PlayerCount(numPlayers)
	order := defaultPlayOrder(n)

	ctx := Ctx{
		NumPlayers:    n,
		CurrentPlayer: order[0],
		PlayOrder:     order,
		PlayOrderPos:  0,
		Turn:          1,
		Phase:         game.startPhase(),
		Seed:          seed,
	}

	var g G
	if game.Setup != nil {
		g = game.Setup(ctx, setupData)
	}

	st := State{G: g, Ctx: ctx}

	// Plugin private state must exist before any hook fires — hooks can
	// reach into mc.Plugins via the API path.
	st = runPluginSetup(game, st)

	// Setup-time hooks are game code and get the same plugin surface and
	// queues a move would — a game that randomises its opening (first
	// player, opening hands) or opens on a prompt does it from here.
	env := newHookEnv(game, st)

	// Apply the active scope's TurnOrder.PlayOrder override and starting
	// position. Same code path is used to enter a phase mid-game.
	st = applyTurnOrderFirst(game, st, env.mcNoEvents(st))

	// Apply Turn.ActivePlayers, if configured for the start scope.
	st = applyActivePlayersFromTurn(game, st)

	// Run the starting phase's OnBegin hook, then the active Turn.OnBegin.
	// Events queued by these hooks go into a fresh queue and are drained
	// here — without this, events.EndTurn() in phase.OnBegin would be
	// silently dropped (BGIO bug #1237).
	st = runPhaseOnBegin(game, st, env)
	st = runTurnOnBegin(game, st, env)
	drainMC := env.mc(st, "")
	st, _ = drainEvents(game, st, drainMC, env)

	// Persist whatever the setup hooks did to plugin data, exactly as Apply
	// does after a move. Without this a plugin whose API isn't a pointer
	// into its private state would lose every setup-time mutation. It must
	// happen before the cascade: each drain step builds its plugin APIs
	// from State.Plugins.
	st = flushPlugins(game, st, drainMC)

	// Harvest the hooks' pushes and blocks and run the cascade. On error
	// drainCascade hands back the pre-cascade state with the moves still
	// queued, which is what NewMatchSeeded keeps (see its doc comment).
	return drainCascade(context.Background(), game, st, env.queue, -1)
}

// PlayerView returns a copy of the state with G and plugin private data
// redacted for the given seat. playerID="" produces the spectator view.
//
// The transport calls this immediately before pushing state to a client.
func PlayerView(game *Game, state State, playerID string) State {
	view := state
	if game.PlayerView != nil {
		view.G = game.PlayerView(state.G, state.Ctx, playerID)
	}
	view.Plugins = redactPluginData(game, state, playerID)
	// Redact per-move args for entries flagged Move.Redact: other seats
	// see the entry's metadata but not the args. This is the seat-level
	// contract LogEntry.Args documents; without it, Move.Redact would
	// be a no-op on the wire.
	view.Log = redactedLog(state.Log, playerID)
	view.Undone = redactedLog(state.Undone, playerID)
	// Run the opt-in per-block redaction hook (nil leaves Blocks as-is —
	// see Game.BlockView). Pending blocks are the one piece of state that
	// PlayerView didn't already cover: a manual-target / selection /
	// pairing prompt queued by Queue.Block, Queue.RequestTarget or
	// Queue.RequestAssignment can carry hidden information in its
	// Data/Target/Assignment payload.
	view.Blocks = redactedBlocks(game, state.Blocks, playerID)
	// Engine-private bookkeeping that should never leave the server.
	view.ActiveStack = nil
	view.PendingNext = nil
	view.MoveCounts = nil
	view.StageMinMoves = nil
	view.StageMaxMoves = nil
	// The per-match secret seed never reaches a client — with it, a
	// player could predict every future shuffle.
	view.Ctx.Seed = 0
	return view
}

// UnmarshalJSON captures the raw bytes of the G field into a private buffer
// so the match manager can later re-decode it through Game.DecodeG into the
// concrete Go type. The rest of the State decodes through the default codec —
// G itself is also decoded with the default behavior (yielding map[string]any
// for the generic G), so games without a DecodeG hook see exactly the same
// shape as before this change.
func (s *State) UnmarshalJSON(b []byte) error {
	type stateAlias State
	var alias stateAlias
	if err := json.Unmarshal(b, &alias); err != nil {
		return err
	}
	var rawHolder struct {
		G       json.RawMessage            `json:"G"`
		Plugins map[string]json.RawMessage `json:"plugins"`
	}
	if err := json.Unmarshal(b, &rawHolder); err != nil {
		return err
	}
	*s = State(alias)
	s.rawG = rawHolder.G
	s.rawPlugins = rawHolder.Plugins
	return nil
}

// RawG returns the raw JSON bytes captured for the G field during the most
// recent UnmarshalJSON, or nil if the state was never unmarshaled (e.g. it
// came from the in-memory store, which preserves the live Go value).
func (s State) RawG() json.RawMessage { return s.rawG }

// ClearRaw drops the captured raw G / plugin bytes. The match manager
// calls it after consuming them in its rehydration pass — the raw
// bytes describe the state AS LOADED, and a match served from a live
// cache keeps its State value (and therefore these fields) across
// moves. Without consume-once semantics every subsequent load
// re-decodes the ORIGINAL snapshot over the live G, silently rolling
// the game back to its as-loaded state while Ctx marches on.
func (s *State) ClearRaw() {
	s.rawG = nil
	s.rawPlugins = nil
}

// RawPlugin returns the raw JSON bytes captured for one plugin's private
// data during the most recent UnmarshalJSON, or nil. The match manager
// passes it to PluginDecode implementations so typed plugin state (e.g.
// the random plugin's PRNG state) survives a persistence round-trip —
// without it, a reloaded match hands plugins map[string]any and typed
// assertions in their API hooks fail.
func (s State) RawPlugin(name string) json.RawMessage {
	return s.rawPlugins[name]
}
