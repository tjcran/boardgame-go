package bots

import (
	"context"
	"sync"
	"time"

	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/match"
	"github.com/tjcran/boardgame-go/storage"
)

// AutoPlayer wraps a Bot so it can sit on a seat in a live match. It
// subscribes to state pushes and calls the bot whenever it's its turn
// (or whenever it's in the active-players set), then submits the move
// through the manager.
//
// One AutoPlayer per seat. They're plain goroutines — run as many as you
// want in the same process.
type AutoPlayer struct {
	Bot         Bot
	Manager     *match.Manager
	MatchID     string
	PlayerID    string
	Credentials string

	// MoveDelay is an optional cosmetic pause before submitting (so
	// human players don't feel they're playing against a millisecond
	// shotgun). Default 0.
	MoveDelay time.Duration

	mu       sync.Mutex
	pending  chan core.State
	stopOnce sync.Once
}

// Send implements match.Subscriber — receives state pushes from the
// manager.
func (a *AutoPlayer) Send(state core.State) {
	a.mu.Lock()
	ch := a.pending
	a.mu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- state:
	default:
		// drop — bots only care about the latest state
		select {
		case <-ch:
		default:
		}
		select {
		case ch <- state:
		default:
		}
	}
}

// SendChat is a no-op; bots don't talk.
func (a *AutoPlayer) SendChat(match.ChatMessage) {}

// SendMatchData is a no-op; bots don't care about player metadata.
func (a *AutoPlayer) SendMatchData([]storage.Player) {}

// PlayerID implements match.Subscriber.
func (a *AutoPlayer) PlayerIDStr() string { return a.PlayerID }

// PlayerID implements match.Subscriber. (interface method)
func (a *AutoPlayer) playerIDMethod() string { return a.PlayerID }

// Run subscribes to the match and acts on every state push where the bot
// is the current player. Returns when ctx is cancelled or the manager
// reports a fatal error.
func (a *AutoPlayer) Run(ctx context.Context) error {
	a.mu.Lock()
	a.pending = make(chan core.State, 1)
	a.mu.Unlock()

	unsub := a.Manager.Subscribe(a.MatchID, autoSubscriber{a: a})
	defer unsub()

	// Prime with the current state in case the match is already
	// awaiting the bot.
	if m, err := a.Manager.State(a.MatchID); err == nil {
		a.Send(m.State)
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case state := <-a.pending:
			if state.Ctx.Gameover != nil {
				return nil
			}
			if !a.isMyTurn(state) {
				continue
			}
			if a.MoveDelay > 0 {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(a.MoveDelay):
				}
			}
			action, err := a.Bot.Play(ctx, state, a.PlayerID)
			if err != nil {
				continue
			}
			_, _ = a.Manager.MoveReqCtx(ctx, a.MatchID, a.PlayerID, a.Credentials,
				core.MoveRequest{
					Move:    action.Move,
					Args:    action.Args,
					StateID: state.StateID,
				})
		}
	}
}

// isMyTurn returns true when this AutoPlayer should act on the given
// state — either we own the current turn or we're in ctx.ActivePlayers.
func (a *AutoPlayer) isMyTurn(state core.State) bool {
	if state.Ctx.ActivePlayers != nil {
		_, ok := state.Ctx.ActivePlayers[a.PlayerID]
		return ok
	}
	return state.Ctx.CurrentPlayer == a.PlayerID
}

// autoSubscriber is the match.Subscriber that wraps an AutoPlayer. We
// can't make AutoPlayer itself satisfy the interface without exposing
// PlayerID() with a conflicting name, so the adapter is a tiny shim.
type autoSubscriber struct{ a *AutoPlayer }

func (s autoSubscriber) Send(state core.State)                       { s.a.Send(state) }
func (s autoSubscriber) SendChat(msg match.ChatMessage)              { s.a.SendChat(msg) }
func (s autoSubscriber) SendMatchData(players []storage.Player)      { s.a.SendMatchData(players) }
func (s autoSubscriber) PlayerID() string                            { return s.a.PlayerID }
