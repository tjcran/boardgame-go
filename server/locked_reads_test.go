package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/match"
	"github.com/tjcran/boardgame-go/storage"
)

// inPlaceState is a pointer G that the game's move changes in place and
// returns, as some embedding games do. Encoding or redacting it while a
// move runs is a data race (and the map makes it a fatal one), so every
// read of it outside a move has to happen under the match lock.
type inPlaceState struct {
	N    int            `json:"n"`
	Seen map[string]int `json:"seen"`
}

// inPlaceGame builds the test game. decode, when non-nil, is called at
// the start of every DecodeG so a test can hold a load mid-decode.
func inPlaceGame(decode func()) *core.Game {
	return &core.Game{
		Name:       "inplace",
		MinPlayers: 1,
		MaxPlayers: 1,
		Setup: func(_ core.Ctx, _ any) core.G {
			return &inPlaceState{Seen: map[string]int{}}
		},
		DecodeG: func(raw json.RawMessage) (core.G, error) {
			if decode != nil {
				decode()
			}
			var s inPlaceState
			if err := json.Unmarshal(raw, &s); err != nil {
				return nil, err
			}
			if s.Seen == nil {
				s.Seen = map[string]int{}
			}
			return &s, nil
		},
		// The redaction reads the map too, like a real hidden-info view.
		PlayerView: func(g core.G, _ core.Ctx, _ string) core.G {
			s := g.(*inPlaceState)
			return &inPlaceState{N: s.N, Seen: map[string]int{"keys": len(s.Seen)}}
		},
		Moves: map[string]any{
			"bump": core.MoveFn(func(mc *core.MoveContext, _ ...any) (core.G, error) {
				s := mc.G.(*inPlaceState)
				s.N++
				s.Seen[strconv.Itoa(s.N%8)] = s.N
				return s, nil
			}),
		},
	}
}

// httpPost is postJSON without t.Fatalf, for use off the test goroutine.
func httpPost(url string, body any) (int, []byte, error) {
	b, _ := json.Marshal(body)
	resp, err := http.Post(url, "application/json", bytes.NewReader(b))
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	return resp.StatusCode, out, err
}

func httpGet(url string) (int, []byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	return resp.StatusCode, out, err
}

// readSync dials the match socket and returns the state of its sync frame.
func readSync(ctx context.Context, url string) (map[string]any, error) {
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		return nil, err
	}
	defer conn.CloseNow()
	for {
		var frame map[string]any
		if err := wsjson.Read(ctx, conn, &frame); err != nil {
			return nil, err
		}
		if frame["type"] == "sync" {
			st, _ := frame["state"].(map[string]any)
			return st, nil
		}
	}
}

// Run with -race. Every read path of the server (the WebSocket sync
// frame, the broadcast update frames, GET match, the REST and dry move
// responses) runs concurrently with moves of a game that changes G in
// place, over storage.Cached, whose loads all return the one live match.
func TestServerReadsDoNotRaceInPlaceMoves(t *testing.T) {
	m := match.NewManager(storage.NewCached(storage.NewMemory(), storage.CacheOpts{}))
	m.MustRegister(inPlaceGame(nil))
	srv := httptest.NewServer(New(m))
	defer srv.Close()

	id, err := m.Create("inplace", match.CreateOptions{NumPlayers: 1})
	if err != nil {
		t.Fatal(err)
	}
	seat, err := m.Join(id, "solo", match.JoinOptions{})
	if err != nil {
		t.Fatal(err)
	}
	base := srv.URL + "/games/inplace/" + id
	seatQuery := "playerID=" + seat.PlayerID + "&credentials=" + seat.PlayerCredentials
	moveBody := map[string]any{"playerID": seat.PlayerID, "credentials": seat.PlayerCredentials, "move": "bump"}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// A long-lived subscriber: its update frames are built from G on
	// every move.
	sub, _, err := websocket.Dial(ctx, wsURLFor(srv.URL, "inplace", id, ""), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sub.CloseNow()
	go func() {
		for {
			var frame map[string]any
			if wsjson.Read(ctx, sub, &frame) != nil {
				return
			}
		}
	}()

	const rounds = 40
	var wg sync.WaitGroup
	errs := make(chan error, 64)
	run := func(name string, f func() error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < rounds; i++ {
				if err := f(); err != nil {
					errs <- fmt.Errorf("%s: %w", name, err)
					return
				}
			}
		}()
	}
	run("ws move", func() error {
		// Moves through the manager directly, so they run alongside the
		// HTTP ones.
		_, err := m.Move(id, seat.PlayerID, seat.PlayerCredentials, "bump", nil)
		return err
	})
	run("rest move", func() error {
		code, body, err := httpPost(base+"/move", moveBody)
		if err == nil && code != http.StatusOK {
			err = fmt.Errorf("status %d: %s", code, body)
		}
		return err
	})
	run("dry move", func() error {
		code, body, err := httpPost(base+"/dryMove", moveBody)
		if err == nil && code != http.StatusOK {
			err = fmt.Errorf("status %d: %s", code, body)
		}
		return err
	})
	run("get match", func() error {
		code, body, err := httpGet(base)
		if err == nil && code != http.StatusOK {
			err = fmt.Errorf("status %d: %s", code, body)
		}
		return err
	})
	run("ws sync (seat)", func() error {
		_, err := readSync(ctx, wsURLFor(srv.URL, "inplace", id, seatQuery))
		return err
	})
	run("ws sync (spectator)", func() error {
		_, err := readSync(ctx, wsURLFor(srv.URL, "inplace", id, ""))
		return err
	})
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

// decodeGate holds the first DecodeG call it sees until released.
type decodeGate struct {
	armed   atomic.Bool
	entered chan struct{}
	release chan struct{}
}

func newDecodeGate() *decodeGate {
	return &decodeGate{entered: make(chan struct{}), release: make(chan struct{})}
}

func (g *decodeGate) hook() {
	if g.armed.CompareAndSwap(true, false) {
		close(g.entered)
		<-g.release
	}
}

// A match loaded from serialized storage still carries its raw G bytes
// until the first load decodes them. If a connection handshake does that
// first load without the match lock, a move can decode, apply and store
// its result in the meantime, and the handshake then writes its own
// decoding of the old bytes over the live match: the move's G is lost.
// The handshake must load under the match lock, so the move waits.
func TestHandshakeLoadDoesNotRollBackConcurrentMove(t *testing.T) {
	flat, err := storage.NewFlatFile(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cached := storage.NewCached(flat, storage.CacheOpts{})
	gate := newDecodeGate()
	m := match.NewManager(cached)
	m.MustRegister(inPlaceGame(gate.hook))
	srv := httptest.NewServer(New(m))
	defer srv.Close()

	id, err := m.Create("inplace", match.CreateOptions{NumPlayers: 1})
	if err != nil {
		t.Fatal(err)
	}
	seat, err := m.Join(id, "solo", match.JoinOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Move(id, seat.PlayerID, seat.PlayerCredentials, "bump", nil); err != nil {
		t.Fatal(err)
	}
	// Drop the cached entry: the next load reads the stored bytes, so the
	// live match carries raw G until something decodes it.
	cached.Invalidate(id)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// A spectator handshake makes the first load and stops mid-decode.
	gate.armed.Store(true)
	handshake := make(chan error, 1)
	go func() {
		_, err := readSync(ctx, wsURLFor(srv.URL, "inplace", id, ""))
		handshake <- err
	}()
	select {
	case <-gate.entered:
	case <-ctx.Done():
		t.Fatal("handshake never reached DecodeG")
	}

	// A move arrives meanwhile. Unlocked, it completes now; locked, it
	// waits for the handshake's load to finish.
	moved := make(chan error, 1)
	go func() {
		code, body, err := httpPost(srv.URL+"/games/inplace/"+id+"/move", map[string]any{
			"playerID": seat.PlayerID, "credentials": seat.PlayerCredentials, "move": "bump",
		})
		if err == nil && code != http.StatusOK {
			err = fmt.Errorf("status %d: %s", code, body)
		}
		moved <- err
	}()
	select {
	case err := <-moved:
		moved <- err // finished first: the unlocked interleaving
	case <-time.After(300 * time.Millisecond):
		// Blocked behind the handshake's lock: the fixed path.
	}
	close(gate.release)

	if err := <-handshake; err != nil {
		t.Fatalf("handshake: %v", err)
	}
	if err := <-moved; err != nil {
		t.Fatalf("move: %v", err)
	}
	snap, err := m.Snapshot(id)
	if err != nil {
		t.Fatal(err)
	}
	if n := snap.State.G.(*inPlaceState).N; n != 2 {
		t.Fatalf("G rolled back: N = %d after two moves (StateID %d)", n, snap.State.StateID)
	}
}

// Run with -race. Update frames must be encoded when the manager pushes
// them (under the match lock), not later on the connection's writer
// goroutine, where encoding G would race the next in-place move.
func TestBroadcastFramesDoNotRaceInPlaceMoves(t *testing.T) {
	// No PlayerView: the pushed state then holds the live G itself, not
	// a redacted copy.
	game := inPlaceGame(nil)
	game.PlayerView = nil
	m := match.NewManager(storage.NewCached(storage.NewMemory(), storage.CacheOpts{}))
	m.MustRegister(game)
	srv := httptest.NewServer(New(m))
	defer srv.Close()
	id, err := m.Create("inplace", match.CreateOptions{NumPlayers: 1})
	if err != nil {
		t.Fatal(err)
	}
	seat, err := m.Join(id, "solo", match.JoinOptions{})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURLFor(srv.URL, "inplace", id, ""), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	if _, err := drainUntilType(ctx, conn, "sync"); err != nil {
		t.Fatal(err)
	}

	const moves = 200
	for i := 0; i < moves; i++ {
		if _, err := m.Move(id, seat.PlayerID, seat.PlayerCredentials, "bump", nil); err != nil {
			t.Fatal(err)
		}
	}
	// The last update must carry the last state. Earlier frames may be
	// dropped by the bounded send buffer; the final one never is.
	for {
		frame, err := drainUntilType(ctx, conn, "update")
		if err != nil {
			t.Fatal(err)
		}
		st, _ := frame["state"].(map[string]any)
		g, _ := st["G"].(map[string]any)
		if n, _ := g["n"].(float64); int(n) == moves {
			return
		}
	}
}

// With delta-state transport the patch must be computed against what
// the client was actually sent. Diffing States that share an in-place G
// compares the live G with itself and drops the change.
func TestDeltaPatchCarriesInPlaceChange(t *testing.T) {
	game := inPlaceGame(nil)
	game.DeltaState = true
	game.PlayerView = nil // so the sent states hold the live G itself
	m := match.NewManager(storage.NewMemory())
	m.MustRegister(game)
	srv := httptest.NewServer(New(m))
	defer srv.Close()
	id, err := m.Create("inplace", match.CreateOptions{NumPlayers: 1})
	if err != nil {
		t.Fatal(err)
	}
	seat, err := m.Join(id, "solo", match.JoinOptions{})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURLFor(srv.URL, "inplace", id, ""), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	if _, err := drainUntilType(ctx, conn, "sync"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Move(id, seat.PlayerID, seat.PlayerCredentials, "bump", nil); err != nil {
		t.Fatal(err)
	}
	frame, err := drainUntilType(ctx, conn, "patch")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(frame["patch"])
	if !bytes.Contains(raw, []byte(`"/G/n"`)) {
		t.Fatalf("patch does not carry the G change: %s", raw)
	}
}
