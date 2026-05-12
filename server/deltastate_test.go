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
		Name:        "delta",
		MinPlayers:  2,
		MaxPlayers:  2,
		DeltaState:  true,
		Setup:       func(_ core.Ctx, _ any) core.G { return &state{} },
		Moves: map[string]any{
			"bump": core.MoveFn(func(mc *core.MoveContext, _ ...any) (core.G, error) {
				s := mc.G.(*state)
				return &state{Count: s.Count + 1}, nil
			}),
		},
		Turn: &core.TurnConfig{MinMoves: 1, MaxMoves: 1},
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
		"/games/delta/" + created.MatchID + "/ws?playerID=0"

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.CloseNow()

	// Drain the initial sync frame.
	var initial map[string]any
	if err := wsjson.Read(ctx, conn, &initial); err != nil {
		t.Fatalf("initial: %v", err)
	}
	if initial["type"] != "sync" {
		t.Fatalf("expected sync, got %v", initial["type"])
	}

	// Submit a move. The transport should send a `patch` frame next.
	postJSON(t, srv.URL+"/games/delta/"+created.MatchID+"/move", map[string]any{
		"playerID":    alice.PlayerID,
		"credentials": alice.PlayerCredentials,
		"move":        "bump",
	})

	var frame map[string]any
	if err := wsjson.Read(ctx, conn, &frame); err != nil {
		t.Fatalf("read: %v", err)
	}
	if frame["type"] != "patch" {
		t.Fatalf("expected patch frame, got %v", frame)
	}
	// The patch should contain at least one operation touching G.Count.
	raw, _ := json.Marshal(frame["patch"])
	if !strings.Contains(string(raw), "Count") {
		t.Fatalf("patch missing Count op: %s", raw)
	}
}
