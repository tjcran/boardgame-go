package core

// QueuedAction is one server-driven move waiting to be applied as part
// of a cascade started by an external move. The drain loop in the
// reducer pops actions in FIFO order until the queue is empty or a
// BlockSpec is added.
type QueuedAction struct {
	PlayerID string `json:"playerID"`
	Move     string `json:"move"`
	Args     []any  `json:"args,omitempty"`
}

// BlockSpec pauses the drain until a future move comes in with a
// matching ResumeTag and PlayerID. Apps queue blocks from inside moves
// (typically a "trigger requires target" effect) and consume them when
// a player input arrives.
//
// Data is opaque to the engine — apps use it to carry context across
// the pause (the source card ID, mode flags, etc.) so the resume move
// can read mc.G or the matching block from State.Blocks. For the
// common "ask a player to pick from a candidate set" case, prefer the
// typed Target field plus Queue.RequestTarget — clients and replays
// see a structured shape they can render and validate.
type BlockSpec struct {
	Tag      string         `json:"tag"`
	PlayerID string         `json:"playerID"`
	Order    int            `json:"order,omitempty"` // reserved; ignored by v1 reducer
	Data     any            `json:"data,omitempty"`
	Target   *TargetRequest `json:"target,omitempty"`
}

// Queue is the API exposed to moves via MoveContext.Queue. Like
// Events, it queues operations for the reducer to drain after the
// move returns — but unlike Events these are full moves, with their
// own validation / plugin pass / log entries.
type Queue struct {
	pending []QueuedAction
	blocks  []BlockSpec
}

// Push schedules a server-driven move. The drain loop applies them
// FIFO. Args are stored verbatim; the move's MoveFn receives them
// like any external move.
func (q *Queue) Push(playerID, move string, args ...any) {
	q.pending = append(q.pending, QueuedAction{
		PlayerID: playerID,
		Move:     move,
		Args:     append([]any(nil), args...),
	})
}

// Block pauses the cascade. The reducer keeps draining queued actions
// up to but not past the point where any block is added; once even one
// block is present the drain halts and persists. The next external
// move whose ResumeTag + PlayerID match the block clears it; if all
// blocks are gone the drain continues.
func (q *Queue) Block(tag, playerID string, data any) {
	q.blocks = append(q.blocks, BlockSpec{
		Tag: tag, PlayerID: playerID, Data: data,
	})
}

// RequestTarget is the typed sibling of Block: it pauses the cascade
// and records a TargetRequest the transport / UI / replay can render
// and validate against. Tag is set to req.Kind, so the resume move's
// MoveRequest.ResumeTag should match req.Kind.
//
// Games that need multiple concurrent requests of the same Kind (rare)
// can disambiguate by either calling Block directly with a unique Tag
// or including a discriminator in req.Data.
//
// The resume move reads the typed request via mc.ResumingBlock.Target
// and validates the player's selection with ValidateSelection.
func (q *Queue) RequestTarget(playerID string, req TargetRequest) {
	r := req
	q.blocks = append(q.blocks, BlockSpec{
		Tag:      req.Kind,
		PlayerID: playerID,
		Target:   &r,
	})
}

// Unblock removes the first matching block from the queue. Most apps
// don't call this directly — the reducer auto-removes a block when a
// move with the matching ResumeTag enters Apply.
func (q *Queue) Unblock(tag, playerID string) bool {
	for i, b := range q.blocks {
		if b.Tag == tag && b.PlayerID == playerID {
			q.blocks = append(q.blocks[:i], q.blocks[i+1:]...)
			return true
		}
	}
	return false
}

// PendingBlocks returns a defensive copy of the current block set.
// Useful inside moves that want to inspect "what's the queue waiting
// for?" before deciding what to push.
func (q *Queue) PendingBlocks() []BlockSpec {
	if len(q.blocks) == 0 {
		return nil
	}
	return append([]BlockSpec(nil), q.blocks...)
}

// redactedBlocks returns the block set filtered through game.BlockView for
// a specific viewer. A nil hook (the default) returns blocks unmodified —
// see Game.BlockView for why that's the safe default. Never mutates the
// input slice or its elements; PlayerView is called once per subscriber
// per broadcast, and the authoritative State.Blocks backing every one of
// those calls must stay intact for the reducer's own resume-tag matching.
func redactedBlocks(game *Game, blocks []BlockSpec, playerID string) []BlockSpec {
	if game.BlockView == nil || len(blocks) == 0 {
		return blocks
	}
	out := make([]BlockSpec, len(blocks))
	for i, b := range blocks {
		out[i] = game.BlockView(b, playerID)
	}
	return out
}

// drain pops the pending actions and blocks added during a move.
// Used internally by the reducer.
func (q *Queue) drain() ([]QueuedAction, []BlockSpec) {
	actions := q.pending
	blocks := q.blocks
	q.pending = nil
	q.blocks = nil
	return actions, blocks
}
