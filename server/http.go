// Package server exposes a Manager over HTTP and WebSocket. Routes are
// versionless and JSON-only — keep it small and replaceable.
package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/match"
	"github.com/tjcran/boardgame-go/storage"
)

// Server bundles a match Manager and exposes ServeHTTP.
type Server struct {
	Manager *match.Manager
	mux     *http.ServeMux
}

// New wires routes onto a fresh mux.
func New(m *match.Manager) *Server {
	s := &Server{Manager: m}
	s.mux = http.NewServeMux()
	s.mux.HandleFunc("/", s.route)
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(w, r) }

// route splits the path manually rather than using a heavy router. URL shapes:
//
//	POST /games/{game}/create
//	GET  /games/{game}/matches
//	POST /games/{game}/{matchID}/join
//	GET  /games/{game}/{matchID}/state
//	POST /games/{game}/{matchID}/move
//	GET  /games/{game}/{matchID}/ws
func (s *Server) route(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 3 || parts[0] != "games" {
		http.NotFound(w, r)
		return
	}
	gameName := parts[1]
	tail := parts[2:]

	switch {
	case len(tail) == 1 && tail[0] == "create" && r.Method == http.MethodPost:
		s.handleCreate(w, r, gameName)
	case len(tail) == 1 && tail[0] == "matches" && r.Method == http.MethodGet:
		s.handleList(w, r, gameName)
	case len(tail) == 2 && tail[1] == "join" && r.Method == http.MethodPost:
		s.handleJoin(w, r, gameName, tail[0])
	case len(tail) == 2 && tail[1] == "state" && r.Method == http.MethodGet:
		s.handleState(w, r, gameName, tail[0])
	case len(tail) == 2 && tail[1] == "move" && r.Method == http.MethodPost:
		s.handleMove(w, r, gameName, tail[0])
	case len(tail) == 2 && tail[1] == "ws" && r.Method == http.MethodGet:
		s.handleWS(w, r, gameName, tail[0])
	default:
		http.NotFound(w, r)
	}
}

type createReq struct {
	NumPlayers int  `json:"numPlayers"`
	SetupData  any  `json:"setupData,omitempty"`
	Unlisted   bool `json:"unlisted,omitempty"`
}
type createResp struct {
	MatchID string `json:"matchID"`
}

func (s *Server) handleCreate(w http.ResponseWriter, r *http.Request, gameName string) {
	var req createReq
	_ = json.NewDecoder(r.Body).Decode(&req) // body optional
	id, err := s.Manager.Create(gameName, req.NumPlayers, req.SetupData)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, createResp{MatchID: id})
}

type matchSummary struct {
	ID       string            `json:"id"`
	GameName string            `json:"gameName"`
	Players  []storage.Player  `json:"players"`
	Ctx      core.Ctx          `json:"ctx"`
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request, gameName string) {
	matches, err := s.Manager.List(gameName)
	if err != nil {
		writeErr(w, err)
		return
	}
	out := make([]matchSummary, 0, len(matches))
	for _, m := range matches {
		out = append(out, matchSummary{
			ID: m.ID, GameName: m.GameName, Players: m.Players, Ctx: m.State.Ctx,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

type joinReq struct {
	PlayerID string `json:"playerID"`
	Name     string `json:"name"`
	Seat     string `json:"seat"`
}

func (s *Server) handleJoin(w http.ResponseWriter, r *http.Request, gameName, matchID string) {
	var req joinReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, err)
		return
	}
	res, err := s.Manager.Join(matchID, req.PlayerID, req.Name, req.Seat)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request, gameName, matchID string) {
	m, err := s.Manager.State(matchID)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, m)
}

type moveReq struct {
	PlayerID string `json:"playerID"`
	Move     string `json:"move"`
	Args     []any  `json:"args"`
}

func (s *Server) handleMove(w http.ResponseWriter, r *http.Request, gameName, matchID string) {
	var req moveReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, err)
		return
	}
	state, err := s.Manager.Move(matchID, req.PlayerID, req.Move, req.Args)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, state)
}

// writeJSON sets content-type and writes the body. Errors here are ignored
// because the connection is the caller's problem at that point.
func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// writeErr maps known error categories to HTTP statuses. Unknown errors are
// 400 — we don't surface 500s for game-rule violations.
func writeErr(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	switch {
	case errors.Is(err, storage.ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, match.ErrUnknownGame):
		status = http.StatusNotFound
	case errors.Is(err, match.ErrSeatTaken),
		errors.Is(err, match.ErrNoSeatsLeft),
		errors.Is(err, match.ErrSeatRequired),
		errors.Is(err, match.ErrUnknownSeat),
		errors.Is(err, core.ErrWrongPlayer),
		errors.Is(err, core.ErrUnknownMove),
		errors.Is(err, core.ErrInvalidMove),
		errors.Is(err, core.ErrGameOver):
		status = http.StatusConflict
	}
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
