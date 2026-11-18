package server

import (
	"context"
	"encoding/json"
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
//
// Frames are encoded to bytes when they are queued, not when they are
// written. The manager calls Send and SendPatch while it holds the match
// lock, and G is shared by reference with the live match (a game may
// change it in place on its next move), so encoding later, on the
// writer goroutine, would read G concurrently with that move. For the
// same reason the delta-state baseline (prev) is kept as encoded bytes
// rather than as a State that shares G with the live match.
type wsClient struct {
	conn     *websocket.Conn
	ctx      context.Context
	playerID string // "" for spectators

	// outMu guards closing out against a concurrent enqueue: the manager
	// can push to a subscriber list it copied just before this client
	// unsubscribed, so a send can arrive after close.
	outMu  sync.Mutex
	closed bool
	out    chan []byte
	done   chan struct{}

	prevMu sync.Mutex
	prev   []byte // encoded State last sent; nil until the first frame
	prevID int    // StateID of prev
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
		out:      make(chan []byte, wsSendBufferSize),
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
		err := c.conn.Write(wctx, websocket.MessageText, frame)
		cancel()
		if err != nil {
			// Tear down — the handleWS read loop will exit when the
			// connection closes.
			c.conn.Close(websocket.StatusGoingAway, "slow writer")
			return
		}
	}
}

// close shuts down the writer goroutine. Idempotent; frames queued
// after close are dropped.
func (c *wsClient) close() {
	c.outMu.Lock()
	defer c.outMu.Unlock()
	if !c.closed {
		c.closed = true
		close(c.out)
	}
}

// enqueue encodes frame and queues it; see enqueueBytes. A frame that
// fails to encode is dropped (the client resyncs on the next state).
func (c *wsClient) enqueue(frame map[string]any) {
	b, err := json.Marshal(frame)
	if err != nil {
		return
	}
	c.enqueueBytes(b)
}

// enqueueBytes is the non-blocking send onto c.out with drop-oldest
// semantics when the buffer is full. State pushes are idempotent (only
// the latest matters), so dropping is correct for them; chat/error
// frames drop too, but the writer loop's deadline will surface the slow
// consumer.
func (c *wsClient) enqueueBytes(frame []byte) {
	c.outMu.Lock()
	defer c.outMu.Unlock()
	if c.closed {
		return
	}
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
	enc, err := json.Marshal(state)
	if err != nil {
		return
	}
	c.sendEncoded("update", enc, state.StateID, nil)
}

// sendEncoded queues a full-state frame (`update` or `sync`) built from
// an already-encoded State and records it as the delta baseline. extra
// adds fields to the frame (the sync frame's matchData and matchID).
func (c *wsClient) sendEncoded(typ string, enc []byte, stateID int, extra map[string]any) {
	frame := map[string]any{"type": typ, "state": json.RawMessage(enc)}
	for k, v := range extra {
		frame[k] = v
	}
	c.prevMu.Lock()
	c.prev, c.prevID = enc, stateID
	c.prevMu.Unlock()
	c.enqueue(frame)
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
//
// The diff runs between encodings, not States: a game that changes G in
// place would otherwise compare the live G against itself and send an
// empty patch for a change it never transmitted.
func (c *wsClient) SendPatch(next core.State) {
	enc, err := json.Marshal(next)
	if err != nil {
		return
	}
	c.prevMu.Lock()
	prev, prevID := c.prev, c.prevID
	c.prevMu.Unlock()
	if prev == nil {
		c.sendEncoded("update", enc, next.StateID, nil)
		return
	}
	patch, err := jsondiff.CompareJSON(prev, enc)
	if err != nil {
		c.sendEncoded("update", enc, next.StateID, nil)
		return
	}
	c.prevMu.Lock()
	c.prev, c.prevID = enc, next.StateID
	c.prevMu.Unlock()
	c.enqueue(map[string]any{
		"type":    "patch",
		"patch":   patch,
		"prevID":  prevID,
		"stateID": next.StateID,
	})
}

// SendMatchData enqueues a matchData frame when the seated-player list
// changes.
func (c *wsClient) SendMatchData(players []storage.Player) {
	c.enqueue(map[string]any{"type": "matchData", "matchData": matchDataFrame(players)})
}

// matchDataFrame is the public per-seat summary sent in sync and
// matchData frames (no credentials, no player Data).
func matchDataFrame(players []storage.Player) []map[string]any {
	out := make([]map[string]any, 0, len(players))
	for _, p := range players {
		out = append(out, map[string]any{
			"id":          p.ID,
			"name":        p.Name,
			"seat":        p.Seat,
			"isConnected": p.IsConnected,
		})
	}
	return out
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
	// ResumeTag resumes a pending interactive block (core.State.Blocks).
	// Without it a match with pending blocks rejects every move with
	// ErrBlocked — interactive prompts would be unresolvable over the
	// wire. Clients set it to the pending block's tag (conventionally
	// the resuming move's name).
	ResumeTag string `json:"resumeTag,omitempty"`
	Payload   any    `json:"payload"`
}

// handleWS upgrades the connection, sends the current state immediately,
// and then loops on incoming move messages.
//
// Authentication contract:
//
//   - The Origin header is checked against the server's configured
//     allow-list before the upgrade. Mismatched origins get 403, which
//     blocks cross-site WebSocket hijacking (an attacker page can open
//     a WS without CORS preflight; the only gate is the Origin check).
//
//   - A seat-bound connection (playerID query param non-empty) must
//     supply matching credentials via the `credentials` query param.
//     Unauthenticated requests for a specific seat are rejected; this
//     prevents unauthenticated readers from receiving that seat's
//     PlayerView (hidden hand, private plugin payloads, redacted log
//     args) and prevents spoofed presence / chat as that seat.
//
//   - Spectators connect with no playerID. They receive the spectator
//     view (PlayerID="" → PlayerView strips hidden state).
func (s *Server) handleWS(w http.ResponseWriter, r *http.Request, gameName, matchID string) {
	// Origin gate. An empty Origin header is allowed (non-browser
	// clients — direct WS libraries don't set Origin and aren't
	// subject to the cross-site attack this guards against).
	if origin := r.Header.Get("Origin"); origin != "" && !s.originAllowed(origin) {
		http.Error(w, "origin not allowed", http.StatusForbidden)
		return
	}

	playerID := r.URL.Query().Get("playerID")
	credentials := r.URL.Query().Get("credentials")

	// Seat-bound connections must authenticate. Reject before the
	// upgrade so the failure surfaces as an HTTP 401 the client can
	// see, rather than an opaque WS close.
	if playerID != "" {
		if !s.Manager.Authenticate(matchID, playerID, credentials) {
			http.Error(w, "invalid credentials", http.StatusUnauthorized)
			return
		}
	}

	// We've already validated the Origin and credentials ourselves; the
	// library's own same-origin check is now redundant.
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

	// Spectator gate (BGIO #1007). When playerID is empty and the game
	// disallows spectators, refuse before any subscribe / sync.
	if playerID == "" {
		var gameName string
		_ = s.Manager.View(matchID, func(m *storage.Match) error {
			gameName = m.GameName
			return nil
		})
		if g := s.Manager.Game(gameName); g != nil &&
			g.SpectatorsAllowed != nil && !*g.SpectatorsAllowed {
			conn.Close(websocket.StatusPolicyViolation, "spectators not allowed")
			return
		}
	}

	client := newWSClient(ctx, conn, playerID)
	defer client.close()

	unsub := s.Manager.Subscribe(matchID, client)
	defer unsub()

	pingCtx, cancelPing := context.WithCancel(ctx)
	defer cancelPing()
	go heartbeat(pingCtx, conn, 25*time.Second)

	// Push the initial sync frame on connect. The view is built and
	// encoded under the match lock (G is shared with the live match),
	// and queued there too, so it is ordered before any update a later
	// move broadcasts. Queueing never blocks; the writer goroutine does
	// the network write.
	_ = s.Manager.View(matchID, func(m *storage.Match) error {
		g := s.Manager.Game(m.GameName)
		if g == nil {
			return nil
		}
		view := core.PlayerView(g, m.State, playerID)
		enc, err := json.Marshal(view)
		if err != nil {
			return err
		}
		client.sendEncoded("sync", enc, view.StateID, map[string]any{
			"matchData": matchDataFrame(m.Players),
			"matchID":   matchID,
		})
		return nil
	})

	_ = s.Manager.SetConnected(matchID, playerID, true)
	defer s.Manager.SetConnected(matchID, playerID, false)

	for {
		var msg inbound
		if err := wsjson.Read(ctx, conn, &msg); err != nil {
			return
		}
		switch msg.Type {
		case "move":
			// MoveReqCtx revalidates msg.Credentials against
			// msg.PlayerID, so move-time auth doesn't rely on
			// the connection-level binding.
			_, err := s.Manager.MoveReqCtx(ctx, matchID, msg.PlayerID, msg.Credentials, core.MoveRequest{
				Move:      msg.Move,
				Args:      msg.Args,
				StateID:   msg.StateID,
				ResumeTag: msg.ResumeTag,
			})
			if err != nil {
				metrics.MovesRejected.Add(1)
				client.sendError(err)
			} else {
				metrics.MovesApplied.Add(1)
			}
		case "chat":
			// Chat carries no built-in credential check inside the
			// Manager, so the transport enforces it. A client may
			// send chat as a different seat from the one this
			// socket authenticated as, provided they prove
			// possession of that seat's credentials.
			if msg.PlayerID != "" && !s.Manager.Authenticate(matchID, msg.PlayerID, msg.Credentials) {
				client.sendError(match.ErrBadCredentials)
				continue
			}
			s.Manager.Chat(matchID, msg.PlayerID, msg.Payload)
			metrics.ChatMessages.Add(1)
		}
	}
}
