package core

// Events is the API exposed to moves and hooks for queueing engine-level
// state transitions. Calls do not take effect immediately — they append to
// a queue that the reducer drains after the move's G changes apply.
//
// Mirrors boardgame.io's `events` argument.
type Events struct {
	queue []event
}

// event is one queued transition. The reducer applies these in order.
type event struct {
	kind eventKind
	// payload fields, used by kind:
	playerID  string
	stage     string
	phase     string
	gameover  any
	turnNext  string
	activeCfg *ActivePlayersConfig
	stageOpts *setStageOpts
}

type eventKind int

const (
	evEndTurn eventKind = iota
	evEndPhase
	evEndStage
	evSetStage
	evSetPhase
	evEndGame
	evSetActivePlayers
	evPass
	evRemovePlayer
)

// setStageOpts carries the optional minMoves/maxMoves for the long-form
// setStage({stage, minMoves, maxMoves}) call.
type setStageOpts struct {
	stage    string
	minMoves int
	maxMoves int
	hasMin   bool
	hasMax   bool
}

// EndTurn queues an end-of-turn transition. If next is non-empty, that
// player becomes the new CurrentPlayer (skipping the configured Order).
func (e *Events) EndTurn(next ...string) {
	ev := event{kind: evEndTurn}
	if len(next) > 0 {
		ev.turnNext = next[0]
	}
	e.queue = append(e.queue, ev)
}

// Pass queues an EndTurn with no override — alias for clarity, matches
// BGIO's events.pass().
func (e *Events) Pass() { e.queue = append(e.queue, event{kind: evPass}) }

// EndPhase queues an end-of-phase transition. The phase's Next field, if
// set, determines what follows.
func (e *Events) EndPhase() {
	e.queue = append(e.queue, event{kind: evEndPhase})
}

// SetPhase queues a transition to a specific phase, ending the current one
// first.
func (e *Events) SetPhase(name string) {
	e.queue = append(e.queue, event{kind: evSetPhase, phase: name})
}

// EndStage queues an end-of-stage transition for the *caller's* player. The
// stage's Next field, if set, determines the player's next stage.
func (e *Events) EndStage() {
	e.queue = append(e.queue, event{kind: evEndStage})
}

// SetStage queues a stage transition for the *caller's* player.
func (e *Events) SetStage(name string) {
	e.queue = append(e.queue, event{kind: evSetStage, stage: name})
}

// SetStageLong is the long-form `setStage({stage, minMoves, maxMoves})`
// equivalent. Pass -1 for unset min/max (only positive values are applied).
func (e *Events) SetStageLong(stage string, minMoves, maxMoves int) {
	opts := &setStageOpts{stage: stage}
	if minMoves >= 0 {
		opts.minMoves = minMoves
		opts.hasMin = true
	}
	if maxMoves >= 0 {
		opts.maxMoves = maxMoves
		opts.hasMax = true
	}
	e.queue = append(e.queue, event{kind: evSetStage, stageOpts: opts})
}

// EndGame queues an end-of-game transition. The argument, if any, becomes
// ctx.Gameover. Calling with no args sets Gameover to a non-nil sentinel
// `true` so callers can distinguish "ended" from "in progress".
func (e *Events) EndGame(result ...any) {
	ev := event{kind: evEndGame, gameover: true}
	if len(result) > 0 {
		ev.gameover = result[0]
	}
	e.queue = append(e.queue, ev)
}

// SetActivePlayers queues an update to ctx.ActivePlayers.
func (e *Events) SetActivePlayers(cfg ActivePlayersConfig) {
	e.queue = append(e.queue, event{kind: evSetActivePlayers, activeCfg: &cfg})
}

// RemovePlayer eliminates a player from the match, splicing them out of
// ctx.PlayOrder so the turn order skips them naturally. If the removed
// player was the current player, the turn ends.
//
// Mirrors the feature request behind BGIO issue #616 — player elimination
// outside the normal game flow.
func (e *Events) RemovePlayer(playerID string) {
	e.queue = append(e.queue, event{kind: evRemovePlayer, playerID: playerID})
}

// drain pops queued events into a slice and clears the queue. Used by the
// reducer.
func (e *Events) drain() []event {
	out := e.queue
	e.queue = nil
	return out
}
