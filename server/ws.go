package server

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/wI2L/jsondiff"

	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/match"
	"github.com/tjcran/boardgame-go/storage"
)

// wsClient is one connected browser tab. The match.Manager pushes state
// snapshots through Send; the connection's read loop receives moves.
//
// Writes go through a bounded send channel drained by a single goroutine,
// so a slow client never blocks the manager's broadcast — and never holds
// the per-match lock that Manager.Move acquires. When the channel fills
// (slow consumer), older state-shaped frames are dropped (we always have
// a fresher one coming); chat/error frames drop too, but the connection
// is closed shortly after via the write loop's error path.
//
// BGIO has this bug structurally — Node's single event loop serialises
// every subscriber's write; we get true per-subscriber concurrency for
// free via goroutines.
type wsClient struct {
	conn     *websocket.Conn
	ctx      context.Context
	playerID string // "" for spectators

	out  chan map[string]any
	done chan struct{}

	prevMu sync.Mutex
	prev   *core.State
}

// wsSendBufferSize is how many pending frames we hold per connection
// before dropping. ~32 messages is plenty for a healthy client; a client
// that builds 32 frames of backlog without acking is by definition stuck.
const wsSendBufferSize = 32

// newWSClient wires the bounded send channel and starts the writer
// goroutine. Caller must call client.close() when the socket closes.
func newWSClient(ctx context.Context, conn *websocket.Conn, playerID string) *wsClient {
	c := &wsClient{
		conn:     conn,
		ctx:      ctx,
		playerID: playerID,
		out:      make(chan map[string]any, wsSendBufferSize),
		done:     make(chan struct{}),
	}
	go c.writeLoop()
	return c
}

// writeLoop drains the send channel onto the WebSocket. Any write error
// (timeout, client gone) tears the connection down and stops the loop.
func (c *wsClient) writeLoop() {
	defer close(c.done)
	for frame := range c.out {
		wctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
		err := wsjson.Write(wctx, c.conn, frame)
		cancel()
		if err != nil {
			// Tear down — the handleWS read loop will exit when the
			// connection closes.
			c.conn.Close(websocket.StatusGoingAway, "slow writer")
			return
		}
	}
}

// close shuts down the writer goroutine. Idempotent.
func (c *wsClient) close() {
	defer func() { recover() }() // close on already-closed channel is OK
	close(c.out)
}

// enqueue is the non-blocking send onto c.out with drop-oldest semantics
// when the buffer is full. State pushes are idempotent (only the latest
// matters), so dropping is correct for them; chat/error frames drop too,
// but the writer loop's deadline will surface the slow consumer.
func (c *wsClient) enqueue(frame map[string]any) {
	select {
	case c.out <- frame:
		return
	default:
	}
	// Full — drop one entry and try again.
	select {
	case <-c.out:
	default:
	}
	select {
	case c.out <- frame:
	default:
		// Still full; give up. The next write timeout will close us.
	}
}

// Send implements match.Subscriber. Mirrors BGIO's `update` frame shape.
func (c *wsClient) Send(state core.State) {
	c.prevMu.Lock()
	cp := state
	c.prev = &cp
	c.prevMu.Unlock()
	c.enqueue(map[string]any{"type": "update", "state": state})
}

// PlayerID identifies which seat this subscriber represents.
func (c *wsClient) PlayerID() string { return c.playerID }

// SendChat enqueues a chat frame.
func (c *wsClient) SendChat(msg match.ChatMessage) {
	c.enqueue(map[string]any{"type": "chat", "chat": msg})
}

// SendPatch diffs against the connection's last sent state and enqueues
// a JSON Patch `patch` frame. If we have no prior state cached, falls
// back to a full state push so clients don't desync.
func (c *wsClient) SendPatch(next core.State) {
	c.prevMu.Lock()
	prev := c.prev
	c.prevMu.Unlock()
	if prev == nil {
		c.Send(next)
		return
	}
	patch, err := jsondiff.Compare(*prev, next)
	if err != nil {
		c.Send(next)
		return
	}
	c.prevMu.Lock()
	cp := next
	c.prev = &cp
	c.prevMu.Unlock()
	c.enqueue(map[string]any{
		"type":    "patch",
		"patch":   patch,
		"prevID":  prev.StateID,
		"stateID": next.StateID,
	})
}

// SendMatchData enqueues a matchData frame when the seated-player list
// changes.
func (c *wsClient) SendMatchData(players []storage.Player) {
	out := make([]map[string]any, 0, len(players))
	for _, p := range players {
		out = append(out, map[string]any{
			"id":          p.ID,
			"name":        p.Name,
			"seat":        p.Seat,
			"isConnected": p.IsConnected,
		})
	}
	c.enqueue(map[string]any{"type": "matchData", "matchData": out})
}

// heartbeat sends WebSocket ping frames at the given interval and tears
// the connection down on failure. coder/websocket's Ping returns when
// the peer's pong is received; failure means the peer is gone.
func heartbeat(ctx context.Context, conn *websocket.Conn, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := conn.Ping(pctx)
			cancel()
			if err != nil {
				conn.Close(websocket.StatusGoingAway, "heartbeat failed")
				return
			}
		}
	}
}

// sendError pushes a {"type":"error","error":...} frame.
func (c *wsClient) sendError(err error) {
	c.enqueue(map[string]any{"type": "error", "error": err.Error()})
}

// inbound is the envelope clients send.
type inbound struct {
	Type        string `json:"type"`
	PlayerID    string `json:"playerID"`
	Credentials string `json:"credentials"`
	Move        string `json:"move"`
	Args        []any  `json:"args"`
	StateID     int    `json:"stateID,omitempty"`
	Payload     any    `json:"payload"`
}

// handleWS upgrades the connection, sends the current state immediately,
// and then loops on incoming move messages.
func (s *Server) handleWS(w http.ResponseWriter, r *http.Request, gameName, matchID string) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		return
	}
	defer conn.CloseNow()
	metrics.WebSocketConns.Add(1)
	defer metrics.WebSocketConns.Add(-1)

	ctx := r.Context()
	playerID := r.URL.Query().Get("playerID")
	client := newWSClient(ctx, conn, playerID)
	defer client.close()

	unsub := s.Manager.Subscribe(matchID, client)
	defer unsub()

	pingCtx, cancelPing := context.WithCancel(ctx)
	defer cancelPing()
	go heartbeat(pingCtx, conn, 25*time.Second)

	// Push the initial sync frame on connect.
	if m, err := s.Manager.State(matchID); err == nil {
		if g := s.Manager.Game(m.GameName); g != nil {
			view := core.PlayerView(g, m.State, playerID)
			players := make([]map[string]any, 0, len(m.Players))
			for _, p := range m.Players {
				players = append(players, map[string]any{
					"id":          p.ID,
					"name":        p.Name,
					"seat":        p.Seat,
					"isConnected": p.IsConnected,
				})
			}
			client.prevMu.Lock()
			cp := view
			client.prev = &cp
			client.prevMu.Unlock()
			client.enqueue(map[string]any{
				"type":      "sync",
				"state":     view,
				"matchData": players,
				"matchID":   matchID,
			})
		}
	}

	_ = s.Manager.SetConnected(matchID, playerID, true)
	defer s.Manager.SetConnected(matchID, playerID, false)

	for {
		var msg inbound
		if err := wsjson.Read(ctx, conn, &msg); err != nil {
			return
		}
		switch msg.Type {
		case "move":
			_, err := s.Manager.MoveReqCtx(ctx, matchID, msg.PlayerID, msg.Credentials, core.MoveRequest{
				Move:    msg.Move,
				Args:    msg.Args,
				StateID: msg.StateID,
			})
			if err != nil {
				metrics.MovesRejected.Add(1)
				client.sendError(err)
			} else {
				metrics.MovesApplied.Add(1)
			}
		case "chat":
			s.Manager.Chat(matchID, msg.PlayerID, msg.Payload)
			metrics.ChatMessages.Add(1)
		}
	}
}
