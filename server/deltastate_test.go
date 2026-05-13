package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/match"
	"github.com/tjcran/boardgame-go/storage"
)

// deltaGame is a 2-player game with DeltaState=true. Each move bumps a
// counter. The transport should send `patch` frames instead of full state
// after the initial sync.
func deltaGame() *core.Game {
	type state struct{ Count int }
	return &core.Game{
		Name:       "delta",
		MinPlayers: 2,
		MaxPlayers: 2,
		DeltaState: true,
		Setup:      func(_ core.Ctx, _ any) core.G { return &state{} },
		Moves: map[string]any{
			"bump": core.MoveFn(func(mc *core.MoveContext, _ ...any) (core.G, error) {
				s := mc.G.(*state)
				return &state{Count: s.Count + 1}, nil
			}),
		},
		Turn: &core.TurnConfig{MinMoves: 1, MaxMoves: 1},
	}
}

// drainUntilType reads frames until one with the wanted type arrives, then
// returns it. Ignores any intervening sync/matchData frames so tests that
// only care about a specific post-event frame don't have to enumerate
// every connect-time frame.
func drainUntilType(ctx context.Context, conn *websocket.Conn, wantType string) (map[string]any, error) {
	for {
		var frame map[string]any
		if err := wsjson.Read(ctx, conn, &frame); err != nil {
			return nil, err
		}
		if frame["type"] == wantType {
			return frame, nil
		}
	}
}

func TestDeltaStateSendsPatchFrame(t *testing.T) {
	m := match.NewManager(storage.NewMemory())
	m.Register(deltaGame())
	srv := httptest.NewServer(New(m))
	defer srv.Close()

	var created struct{ MatchID string }
	decode(t, postJSON(t, srv.URL+"/games/delta/create", nil), &created)
	alice := joinByHTTP(t, srv.URL, "delta", created.MatchID, "alice")
	joinByHTTP(t, srv.URL, "delta", created.MatchID, "bob")

	wsURL := strings.Replace(srv.URL, "http://", "ws://", 1) +
		"/games/delta/" + created.MatchID +
		"/ws?playerID=" + alice.PlayerID +
		"&credentials=" + alice.PlayerCredentials

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.CloseNow()

	// Wait until the connect-time frames (sync + matchData(connected=true))
	// have arrived. We test by waiting briefly — alternative would be to
	// pump frames here, but the next move will produce a patch frame and
	// drainUntilType handles the rest.
	time.Sleep(50 * time.Millisecond)

	// Submit a move. The transport should send a `patch` frame next.
	postJSON(t, srv.URL+"/games/delta/"+created.MatchID+"/move", map[string]any{
		"playerID":    alice.PlayerID,
		"credentials": alice.PlayerCredentials,
		"move":        "bump",
	})

	frame, err := drainUntilType(ctx, conn, "patch")
	if err != nil {
		t.Fatalf("drain: %v", err)
	}
	raw, _ := json.Marshal(frame["patch"])
	if !strings.Contains(string(raw), "Count") {
		t.Fatalf("patch missing Count op: %s", raw)
	}
}
