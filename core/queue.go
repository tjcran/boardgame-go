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
	Tag      string `json:"tag"`
	PlayerID string `json:"playerID"`
	// Move, when set, names the move that answers this block (see
	// AnsweredBy). Only a MoveRequest for that move can consume the
	// block with its ResumeTag, and only a named answer is exempt from
	// an Exclusive stage's move table. Empty means any move carrying
	// the tag may consume the block, as before Move existed.
	Move   string         `json:"move,omitempty"`
	Order  int            `json:"order,omitempty"` // reserved; ignored by v1 reducer
	Data   any            `json:"data,omitempty"`
	Target *TargetRequest `json:"target,omitempty"`
	// Assignment is set by Queue.RequestAssignment: a typed pairing
	// prompt the resume move validates with ValidateAssignment.
	Assignment *AssignmentRequest `json:"assignment,omitempty"`
}

// BlockOption configures a block raised with Queue.Block or
// Queue.RequestTarget.
type BlockOption func(*BlockSpec)

// AnsweredBy names the move that answers a block. The reducer then
// matches a ResumeTag only on a request for that move, so the prompt
// cannot be consumed by some other move that merely carries its tag,
// and the answer stays legal for a player confined to an Exclusive
// stage even when the move is registered outside the stage's table.
// Clients see the name in State.Blocks, so a UI can tell which move
// resolves the prompt.
func AnsweredBy(move string) BlockOption {
	return func(b *BlockSpec) { b.Move = move }
}

func newBlock(b BlockSpec, opts []BlockOption) BlockSpec {
	for _, opt := range opts {
		opt(&b)
	}
	return b
}

// Queue is the API exposed to moves and hooks via MoveContext.Queue.
// Like Events, it queues operations for the reducer to drain after the
// move returns — but unlike Events these are full moves, with their
// own validation / plugin pass / log entries.
//
// Hooks share the Queue of the move whose events fired them (NewMatch
// and ForceEndTurn hold one for the hooks they run outside a move), so
// a hook can push moves and raise blocks too. The reducer harvests
// them after the move's events have drained, after whatever the move
// itself queued. Because hooks run inside that event drain, where a
// hook raises a block decides what it pauses:
//
//   - Turn.OnBegin: the block pauses the new turn's cascade until its
//     addressee answers it, which is the natural start-of-turn prompt.
//     Non-resume moves are refused with ErrBlocked meanwhile, and the
//     addressee may answer out of turn.
//   - Turn.OnEnd: the block does NOT suspend the turn rotation. The
//     turn still passes and the next turn's OnBegin runs in the same
//     drain; the block only pauses the cascade afterwards, with the new
//     turn under way. A game that needs the ending player's input
//     before the turn passes should collect it in the move that ends
//     the turn.
//
// A block raised during a move that calls MoveContext.DropBlocksFor
// survives the drop: the drop applies to blocks already pending before
// the move, and new blocks merge after it. ForceEndTurn drops the
// ending player's pending blocks the same way, so a block its hooks
// raise for the next turn survives.
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
// move whose ResumeTag + PlayerID match the block clears it (and whose
// Move matches, when the block names its answer via AnsweredBy); if
// all blocks are gone the drain continues.
//
// playerID is the seat that must answer. Name it explicitly from a turn
// or phase hook, where mc.PlayerID is "": a block addressed to "" can be
// answered by no seat.
func (q *Queue) Block(tag, playerID string, data any, opts ...BlockOption) {
	q.blocks = append(q.blocks, newBlock(BlockSpec{
		Tag: tag, PlayerID: playerID, Data: data,
	}, opts))
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
// and validates the player's selection with ValidateSelection. Pass
// AnsweredBy to name that move.
func (q *Queue) RequestTarget(playerID string, req TargetRequest, opts ...BlockOption) {
	r := req
	q.blocks = append(q.blocks, newBlock(BlockSpec{
		Tag:      req.Kind,
		PlayerID: playerID,
		Target:   &r,
	}, opts))
}

// RequestAssignment is the pairing sibling of RequestTarget: it
// pauses the cascade and records an AssignmentRequest the transport /
// UI / replay can render and validate against. Tag is set to
// req.Kind, so the resume move's MoveRequest.ResumeTag should match
// req.Kind.
//
// The resume move reads the typed request via
// mc.ResumingBlock.Assignment and validates the player's pairs with
// ValidateAssignment. Pass AnsweredBy to name that move.
func (q *Queue) RequestAssignment(playerID string, req AssignmentRequest, opts ...BlockOption) {
	r := req
	q.blocks = append(q.blocks, newBlock(BlockSpec{
		Tag:        req.Kind,
		PlayerID:   playerID,
		Assignment: &r,
	}, opts))
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
// see Game.BlockView for why that's the safe default.
//
// redactedBlocks itself never mutates the input slice or its elements: it
// always builds a fresh output slice, and each element starts as a plain
// struct copy from the source. That copy still shares Data/Target with the
// authoritative State.Blocks entry, though — see BlockViewFn's doc for why
// the hook itself must not mutate through them. PlayerView is called once
// per subscriber per broadcast, and the authoritative State.Blocks backing
// every one of those calls must stay intact for the reducer's own
// resume-tag matching.
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
