package core

// State is the complete authoritative state of a match. The transport sends
// (a redacted view of) this object to clients.
type State struct {
	// G is the user-defined game state.
	G G `json:"G"`

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
	// reducer to drain. Populated by moves calling mc.Queue.Push;
	// drained by the engine after each external move's event drain.
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
// through to Game.Setup; pass nil if the game doesn't use one.
func NewMatch(game *Game, numPlayers int, setupData any) State {
	n := game.PlayerCount(numPlayers)
	order := defaultPlayOrder(n)

	ctx := Ctx{
		NumPlayers:    n,
		CurrentPlayer: order[0],
		PlayOrder:     order,
		PlayOrderPos:  0,
		Turn:          1,
		Phase:         game.startPhase(),
	}

	var g G
	if game.Setup != nil {
		g = game.Setup(ctx, setupData)
	}

	st := State{G: g, Ctx: ctx}

	// Plugin private state must exist before any hook fires — hooks can
	// reach into mc.Plugins via the API path.
	st = runPluginSetup(game, st)

	// Apply the active scope's TurnOrder.PlayOrder override and starting
	// position. Same code path is used to enter a phase mid-game.
	mc := &MoveContext{G: st.G, Ctx: st.Ctx}
	st = applyTurnOrderFirst(game, st, mc)

	// Apply Turn.ActivePlayers, if configured for the start scope.
	st = applyActivePlayersFromTurn(game, st)

	// Run the starting phase's OnBegin hook, then the active Turn.OnBegin.
	// Events queued by these hooks go into a fresh queue and are drained
	// here — without this, events.EndTurn() in phase.OnBegin would be
	// silently dropped (BGIO bug #1237).
	events := &Events{}
	st = runPhaseOnBegin(game, st, events)
	st = runTurnOnBegin(game, st, events)
	drainMC := &MoveContext{G: st.G, Ctx: st.Ctx, Events: events}
	st, _ = drainEvents(game, st, drainMC, events)
	return st
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
	// Engine-private bookkeeping that should never leave the server.
	view.ActiveStack = nil
	view.PendingNext = nil
	view.MoveCounts = nil
	view.StageMinMoves = nil
	view.StageMaxMoves = nil
	return view
}
