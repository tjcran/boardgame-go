// Package server exposes a Manager over HTTP and WebSocket. Routes mirror
// boardgame.io's Lobby API exactly so any BGIO client can drive a
// boardgame-go server.
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

// Server bundles a match Manager and CORS Origins, and exposes ServeHTTP.
type Server struct {
	Manager *match.Manager
	Origins []string
	mux     *http.ServeMux
}

// New wires routes onto a fresh mux.
func New(m *match.Manager) *Server {
	s := &Server{Manager: m}
	s.mux = http.NewServeMux()
	s.mux.HandleFunc("/", s.route)
	return s
}

// ServeHTTP runs the request through the CORS middleware then the router.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !s.applyCORS(w, r) {
		return
	}
	s.mux.ServeHTTP(w, r)
}

// route dispatches by URL path and method. URL shapes mirror BGIO's
// `/games/:name/...` Lobby API.
//
//	GET  /games                        — list registered game names
//	GET  /games/{name}                 — list matches
//	GET  /games/{name}/{id}            — get one match
//	POST /games/{name}/create          — create match
//	POST /games/{name}/{id}/join       — join
//	POST /games/{name}/{id}/leave      — leave
//	POST /games/{name}/{id}/update     — update player metadata
//	POST /games/{name}/{id}/playAgain  — start a successor match
//	POST /games/{name}/{id}/move       — submit move (REST; also via WS)
//	GET  /games/{name}/{id}/ws         — WebSocket transport
func (s *Server) route(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 1 || parts[0] != "games" {
		http.NotFound(w, r)
		return
	}
	if len(parts) == 1 && r.Method == http.MethodGet {
		s.handleListGames(w, r)
		return
	}
	if len(parts) < 2 {
		http.NotFound(w, r)
		return
	}
	gameName := parts[1]
	tail := parts[2:]

	switch {
	case len(tail) == 0 && r.Method == http.MethodGet:
		s.handleListMatches(w, r, gameName)
	case len(tail) == 1 && tail[0] == "create" && r.Method == http.MethodPost:
		s.handleCreate(w, r, gameName)
	case len(tail) == 1 && r.Method == http.MethodGet:
		s.handleGetMatch(w, r, gameName, tail[0])
	case len(tail) == 2 && tail[1] == "join" && r.Method == http.MethodPost:
		s.handleJoin(w, r, gameName, tail[0])
	case len(tail) == 2 && tail[1] == "leave" && r.Method == http.MethodPost:
		s.handleLeave(w, r, gameName, tail[0])
	case len(tail) == 2 && tail[1] == "update" && r.Method == http.MethodPost:
		s.handleUpdate(w, r, gameName, tail[0])
	case len(tail) == 2 && tail[1] == "playAgain" && r.Method == http.MethodPost:
		s.handlePlayAgain(w, r, gameName, tail[0])
	case len(tail) == 2 && tail[1] == "move" && r.Method == http.MethodPost:
		s.handleMove(w, r, gameName, tail[0])
	case len(tail) == 2 && tail[1] == "ws" && r.Method == http.MethodGet:
		s.handleWS(w, r, gameName, tail[0])
	default:
		http.NotFound(w, r)
	}
}

// ---- create ---------------------------------------------------------------

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
	id, err := s.Manager.Create(gameName, match.CreateOptions{
		NumPlayers: req.NumPlayers,
		SetupData:  req.SetupData,
		Unlisted:   req.Unlisted,
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	metrics.MatchesCreated.Add(1)
	writeJSON(w, http.StatusCreated, createResp{MatchID: id})
}

// ---- list games -----------------------------------------------------------

func (s *Server) handleListGames(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.Manager.GameNames())
}

// ---- list matches ---------------------------------------------------------

type matchSummary struct {
	MatchID   string           `json:"matchID"`
	Players   []playerSummary  `json:"players"`
	SetupData any              `json:"setupData,omitempty"`
	GameName  string           `json:"gameName"`
	CreatedAt int64            `json:"createdAt"`
	Ctx       core.Ctx         `json:"ctx,omitempty"`
}

type playerSummary struct {
	ID          string `json:"id"`
	Name        string `json:"name,omitempty"`
	Seat        string `json:"seat"`
	IsConnected bool   `json:"isConnected,omitempty"`
	Data        any    `json:"data,omitempty"`
}

func (s *Server) handleListMatches(w http.ResponseWriter, _ *http.Request, gameName string) {
	matches, err := s.Manager.List(gameName)
	if err != nil {
		writeErr(w, err)
		return
	}
	out := struct {
		Matches []matchSummary `json:"matches"`
	}{Matches: make([]matchSummary, 0, len(matches))}
	for _, mm := range matches {
		if mm.Unlisted {
			continue
		}
		out.Matches = append(out.Matches, toSummary(mm))
	}
	writeJSON(w, http.StatusOK, out)
}

// ---- get one match --------------------------------------------------------

func (s *Server) handleGetMatch(w http.ResponseWriter, _ *http.Request, _, matchID string) {
	m, err := s.Manager.State(matchID)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toSummary(m))
}

func toSummary(m *storage.Match) matchSummary {
	out := matchSummary{
		MatchID: m.ID, GameName: m.GameName, SetupData: m.SetupData,
		CreatedAt: m.CreatedAt, Ctx: m.State.Ctx,
		Players: make([]playerSummary, 0, len(m.Players)),
	}
	for _, p := range m.Players {
		out.Players = append(out.Players, playerSummary{
			ID: p.ID, Name: p.Name, Seat: p.Seat,
			IsConnected: p.IsConnected, Data: p.Data,
		})
	}
	return out
}

// ---- join -----------------------------------------------------------------

type joinReq struct {
	PlayerName string `json:"playerName"`
	PlayerID   string `json:"playerID,omitempty"`
	Data       any    `json:"data,omitempty"`
}

func (s *Server) handleJoin(w http.ResponseWriter, r *http.Request, _, matchID string) {
	var req joinReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, err)
		return
	}
	res, err := s.Manager.Join(matchID, req.PlayerName, match.JoinOptions{
		PlayerID: req.PlayerID, Data: req.Data,
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	metrics.MatchesJoined.Add(1)
	writeJSON(w, http.StatusOK, res)
}

// ---- leave ----------------------------------------------------------------

type leaveReq struct {
	PlayerID    string `json:"playerID"`
	Credentials string `json:"credentials"`
}

func (s *Server) handleLeave(w http.ResponseWriter, r *http.Request, _, matchID string) {
	var req leaveReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, err)
		return
	}
	if err := s.Manager.Leave(matchID, req.PlayerID, req.Credentials); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct{}{})
}

// ---- update player --------------------------------------------------------

type updateReq struct {
	PlayerID    string  `json:"playerID"`
	Credentials string  `json:"credentials"`
	NewName     *string `json:"newName,omitempty"`
	Data        any     `json:"data,omitempty"`
	HasData     bool    `json:"-"` // set by the decoder helper below
}

func (s *Server) handleUpdate(w http.ResponseWriter, r *http.Request, _, matchID string) {
	// Decode into a map so we can distinguish missing data vs. explicit null.
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		writeErr(w, err)
		return
	}
	var req updateReq
	if v, ok := raw["playerID"]; ok {
		_ = json.Unmarshal(v, &req.PlayerID)
	}
	if v, ok := raw["credentials"]; ok {
		_ = json.Unmarshal(v, &req.Credentials)
	}
	if v, ok := raw["newName"]; ok {
		var s2 string
		_ = json.Unmarshal(v, &s2)
		req.NewName = &s2
	}
	var data any
	if v, ok := raw["data"]; ok {
		req.HasData = true
		_ = json.Unmarshal(v, &data)
	}
	if err := s.Manager.UpdatePlayer(matchID, req.PlayerID, req.Credentials,
		match.UpdatePlayerOpts{NewName: req.NewName, Data: data, HasData: req.HasData}); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct{}{})
}

// ---- play again -----------------------------------------------------------

type playAgainReq struct {
	PlayerID    string `json:"playerID"`
	Credentials string `json:"credentials"`
	NumPlayers  int    `json:"numPlayers,omitempty"`
	SetupData   any    `json:"setupData,omitempty"`
}
type playAgainResp struct {
	NextMatchID string `json:"nextMatchID"`
}

func (s *Server) handlePlayAgain(w http.ResponseWriter, r *http.Request, _, matchID string) {
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		writeErr(w, err)
		return
	}
	var req playAgainReq
	if v, ok := raw["playerID"]; ok {
		_ = json.Unmarshal(v, &req.PlayerID)
	}
	if v, ok := raw["credentials"]; ok {
		_ = json.Unmarshal(v, &req.Credentials)
	}
	if v, ok := raw["numPlayers"]; ok {
		_ = json.Unmarshal(v, &req.NumPlayers)
	}
	useDataFromPrev := true
	if v, ok := raw["setupData"]; ok {
		_ = json.Unmarshal(v, &req.SetupData)
		useDataFromPrev = false
	}
	next, err := s.Manager.PlayAgain(matchID, req.PlayerID, req.Credentials,
		req.NumPlayers, req.SetupData, useDataFromPrev)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, playAgainResp{NextMatchID: next})
}

// ---- move (REST) ----------------------------------------------------------

type moveReq struct {
	PlayerID    string `json:"playerID"`
	Credentials string `json:"credentials"`
	Move        string `json:"move"`
	Args        []any  `json:"args"`
	StateID     int    `json:"stateID,omitempty"`
}

func (s *Server) handleMove(w http.ResponseWriter, r *http.Request, _, matchID string) {
	var req moveReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, err)
		return
	}
	state, err := s.Manager.MoveReqCtx(r.Context(), matchID, req.PlayerID, req.Credentials, core.MoveRequest{
		Move:    req.Move,
		Args:    req.Args,
		StateID: req.StateID,
	})
	if err != nil {
		metrics.MovesRejected.Add(1)
		writeErr(w, err)
		return
	}
	metrics.MovesApplied.Add(1)
	// Send the requesting player's view back so a REST-only client also
	// sees redacted state.
	if m, err := s.Manager.State(matchID); err == nil {
		if g := s.Manager.Game(m.GameName); g != nil {
			state = core.PlayerView(g, state, req.PlayerID)
		}
	}
	writeJSON(w, http.StatusOK, state)
}

// ---- shared utilities -----------------------------------------------------

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
	case errors.Is(err, match.ErrBadCredentials):
		status = http.StatusUnauthorized
	case errors.Is(err, match.ErrSeatTaken),
		errors.Is(err, match.ErrNoSeatsLeft),
		errors.Is(err, match.ErrSeatRequired),
		errors.Is(err, match.ErrUnknownSeat),
		errors.Is(err, core.ErrWrongPlayer),
		errors.Is(err, core.ErrUnknownMove),
		errors.Is(err, core.ErrInvalidMove),
		errors.Is(err, core.ErrInactivePlayer),
		errors.Is(err, core.ErrGameOver):
		status = http.StatusConflict
	}
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
