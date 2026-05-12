package server

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/tjcran/boardgame-go/core"
)

// wsClient is one connected browser tab. The match.Manager pushes state
// snapshots through Send; the connection's read loop receives moves.
type wsClient struct {
	mu       sync.Mutex
	conn     *websocket.Conn
	ctx      context.Context
	playerID string // "" for spectators
}

// Send implements match.Subscriber. The mutex matters because the manager
// fans out to subscribers without coordinating with the read loop.
func (c *wsClient) Send(state core.State) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
	defer cancel()
	_ = wsjson.Write(ctx, c.conn, map[string]any{"type": "state", "state": state})
}

// PlayerID identifies which seat this subscriber represents so the manager
// can compute the right per-seat state.
func (c *wsClient) PlayerID() string { return c.playerID }

// inbound is the envelope clients send. Only "move" is handled today; future
// versions can extend (chat, leave, etc).
type inbound struct {
	Type     string `json:"type"`
	PlayerID string `json:"playerID"`
	Move     string `json:"move"`
	Args     []any  `json:"args"`
}

// handleWS upgrades the connection, sends the current state immediately, and
// then loops on incoming move messages. The connection's seat is taken
// from the ?playerID= query parameter (empty = spectator).
func (s *Server) handleWS(w http.ResponseWriter, r *http.Request, gameName, matchID string) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// MVP: accept any origin. Tighten when we add auth.
		InsecureSkipVerify: true,
	})
	if err != nil {
		return
	}
	defer conn.CloseNow()

	ctx := r.Context()
	playerID := r.URL.Query().Get("playerID")
	client := &wsClient{conn: conn, ctx: ctx, playerID: playerID}

	// Push the current state on connect so the client can render immediately,
	// redacted for the connecting seat.
	if m, err := s.Manager.State(matchID); err == nil {
		if g := s.Manager.Game(m.GameName); g != nil {
			client.Send(core.PlayerView(g, m.State, playerID))
		}
	}

	unsub := s.Manager.Subscribe(matchID, client)
	defer unsub()

	for {
		var msg inbound
		if err := wsjson.Read(ctx, conn, &msg); err != nil {
			return
		}
		if msg.Type != "move" {
			continue
		}
		if _, err := s.Manager.Move(matchID, msg.PlayerID, msg.Move, msg.Args); err != nil {
			// Surface as an error frame; don't tear down the connection.
			client.mu.Lock()
			ec, cancel := context.WithTimeout(ctx, 5*time.Second)
			_ = wsjson.Write(ec, conn, map[string]string{"type": "error", "error": err.Error()})
			cancel()
			client.mu.Unlock()
		}
	}
}
