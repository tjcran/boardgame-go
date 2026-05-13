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
type wsClient struct {
	mu       sync.Mutex
	conn     *websocket.Conn
	ctx      context.Context
	playerID string // "" for spectators

	// prev is the last redacted state we sent this client. Used by
	// SendPatch to compute JSON Patch diffs when Game.DeltaState=true.
	prev *core.State
}

// Send implements match.Subscriber. The mutex matters because the manager
// fans out to subscribers without coordinating with the read loop.
//
// The wire frame is BGIO's `update` shape: { type: "update", state, matchID }.
// The initial state push at connect time uses `sync` (see handleWS).
func (c *wsClient) Send(state core.State) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cp := state
	c.prev = &cp
	ctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
	defer cancel()
	_ = wsjson.Write(ctx, c.conn, map[string]any{
		"type":  "update",
		"state": state,
	})
}

// PlayerID identifies which seat this subscriber represents so the manager
// can compute the right per-seat state.
func (c *wsClient) PlayerID() string { return c.playerID }

// SendChat implements match.Subscriber; pushes a chat frame to the
// connection.
func (c *wsClient) SendChat(msg match.ChatMessage) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
	defer cancel()
	_ = wsjson.Write(ctx, c.conn, map[string]any{"type": "chat", "chat": msg})
}

// SendPatch implements match.PatchSubscriber. Diffs against the connection's
// last sent state and sends a JSON Patch (RFC 6902) `patch` frame. If we
// don't have a previous state cached (e.g. the connection just opened
// before the first update), falls back to a full state push so clients
// don't desync.
func (c *wsClient) SendPatch(next core.State) {
	c.mu.Lock()
	prev := c.prev
	c.mu.Unlock()
	if prev == nil {
		c.Send(next)
		return
	}
	patch, err := jsondiff.Compare(*prev, next)
	if err != nil {
		c.Send(next)
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	cp := next
	c.prev = &cp
	ctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
	defer cancel()
	_ = wsjson.Write(ctx, c.conn, map[string]any{
		"type":    "patch",
		"patch":   patch,
		"prevID":  prev.StateID,
		"stateID": next.StateID,
	})
}

// SendMatchData implements match.Subscriber; pushes a matchData frame
// when the seated player list changes (BGIO's `matchData` frame).
func (c *wsClient) SendMatchData(players []storage.Player) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
	defer cancel()
	out := make([]map[string]any, 0, len(players))
	for _, p := range players {
		out = append(out, map[string]any{
			"id":          p.ID,
			"name":        p.Name,
			"seat":        p.Seat,
			"isConnected": p.IsConnected,
		})
	}
	_ = wsjson.Write(ctx, c.conn, map[string]any{
		"type":      "matchData",
		"matchData": out,
	})
}

// inbound is the envelope clients send. Today: "move" (submit a move) and
// "chat" (broadcast a chat message). The "leave" wire form is handled by
// the REST API.
type inbound struct {
	Type        string `json:"type"`
	PlayerID    string `json:"playerID"`
	Credentials string `json:"credentials"`
	Move        string `json:"move"`
	Args        []any  `json:"args"`
	Payload     any    `json:"payload"` // chat body
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

	// Subscribe so the matchData broadcast from SetConnected reaches us.
	unsub := s.Manager.Subscribe(matchID, client)
	defer unsub()

	// Spawn a heartbeat that pings every 25s. A failed ping cancels the
	// connection context, which unblocks the read loop and lets the
	// deferred SetConnected(false) flip the flag. 25s is below the
	// commonly seen 30s idle-cutoff on load balancers.
	pingCtx, cancelPing := context.WithCancel(ctx)
	defer cancelPing()
	go heartbeat(pingCtx, conn, 25*time.Second)

	// Push the initial sync frame on connect (BGIO's `sync` payload: full
	// state + matchData). Client.Send is used for subsequent updates.
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
			client.mu.Lock()
			cp := view
			client.prev = &cp // seed for subsequent SendPatch diffs
			ctx2, cancel := context.WithTimeout(ctx, 5*time.Second)
			_ = wsjson.Write(ctx2, conn, map[string]any{
				"type":      "sync",
				"state":     view,
				"matchData": players,
				"matchID":   matchID,
			})
			cancel()
			client.mu.Unlock()
		}
	}

	// Now that sync has been pushed, flip the connected flag. The
	// resulting matchData broadcast arrives next on the wire — order is
	// sync, then matchData(connected=true), then any subsequent updates.
	_ = s.Manager.SetConnected(matchID, playerID, true)
	defer s.Manager.SetConnected(matchID, playerID, false)

	for {
		var msg inbound
		if err := wsjson.Read(ctx, conn, &msg); err != nil {
			return
		}
		switch msg.Type {
		case "move":
			if _, err := s.Manager.Move(matchID, msg.PlayerID, msg.Credentials, msg.Move, msg.Args); err != nil {
				client.sendError(err)
			}
		case "chat":
			s.Manager.Chat(matchID, msg.PlayerID, msg.Payload)
		}
	}
}

// heartbeat sends WebSocket ping frames at the given interval and tears the
// connection down on failure. coder/websocket's Ping returns when the
// peer's pong is received; if the peer is gone or stalled, the call
// returns an error and we close, which unblocks the read loop in handleWS.
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

// sendError pushes a {"type":"error","error":...} frame to the connection
// without tearing it down. The match's authoritative state remains valid.
func (c *wsClient) sendError(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ec, cancel := context.WithTimeout(c.ctx, 5*time.Second)
	defer cancel()
	_ = wsjson.Write(ec, c.conn, map[string]string{"type": "error", "error": err.Error()})
}
