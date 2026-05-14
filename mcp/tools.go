package mcp

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/match"
)

// Tools is the set of MCP tool handlers backed by a match.Manager.
//
// Methods are plain Go functions returning JSON-marshallable structs.
// The MCP SDK glue in server.go adapts these to MCP tool registrations;
// keeping the handlers SDK-free lets tests run without any MCP dependency
// and lets us swap the SDK locally if needed.
type Tools struct {
	Manager *match.Manager
}

// ----- list_games -----

type ListGamesResult struct {
	Games []GameInfo `json:"games"`
}

type GameInfo struct {
	Name       string `json:"name"`
	MinPlayers int    `json:"minPlayers,omitempty"`
	MaxPlayers int    `json:"maxPlayers,omitempty"`
}

// ListGames returns metadata for every game registered on the Manager.
// Sorted by name for deterministic output.
func (t *Tools) ListGames(_ context.Context) (ListGamesResult, error) {
	names := t.Manager.GameNames()
	out := ListGamesResult{Games: make([]GameInfo, 0, len(names))}
	for _, name := range names {
		g := t.Manager.Game(name)
		if g == nil {
			continue
		}
		out.Games = append(out.Games, GameInfo{
			Name:       g.Name,
			MinPlayers: g.MinPlayers,
			MaxPlayers: g.MaxPlayers,
		})
	}
	sort.Slice(out.Games, func(i, j int) bool { return out.Games[i].Name < out.Games[j].Name })
	return out, nil
}

// ----- create_match -----

type CreateMatchArgs struct {
	Game       string `json:"game"`
	NumPlayers int    `json:"numPlayers,omitempty"`
	SetupData  any    `json:"setupData,omitempty"`
	Name       string `json:"name,omitempty"`
}

type CreateMatchResult struct {
	MatchID    string `json:"matchID"`
	Game       string `json:"game"`
	NumPlayers int    `json:"numPlayers"`
}

func (t *Tools) CreateMatch(_ context.Context, args CreateMatchArgs) (CreateMatchResult, error) {
	if args.Game == "" {
		return CreateMatchResult{}, errors.New("game is required")
	}
	matchID, err := t.Manager.Create(args.Game, match.CreateOptions{
		NumPlayers: args.NumPlayers,
		SetupData:  args.SetupData,
		Name:       args.Name,
	})
	if err != nil {
		return CreateMatchResult{}, err
	}
	m, err := t.Manager.State(matchID)
	if err != nil {
		return CreateMatchResult{}, err
	}
	return CreateMatchResult{
		MatchID:    matchID,
		Game:       args.Game,
		NumPlayers: m.State.Ctx.NumPlayers,
	}, nil
}

// ----- join_match -----

type JoinMatchArgs struct {
	MatchID  string `json:"matchID"`
	PlayerID string `json:"playerID,omitempty"`
	Name     string `json:"name,omitempty"`
}

type JoinMatchResult struct {
	MatchID     string `json:"matchID"`
	PlayerID    string `json:"playerID"`
	Seat        string `json:"seat"`
	Credentials string `json:"credentials"`
}

func (t *Tools) JoinMatch(_ context.Context, args JoinMatchArgs) (JoinMatchResult, error) {
	if args.MatchID == "" {
		return JoinMatchResult{}, errors.New("matchID is required")
	}
	res, err := t.Manager.Join(args.MatchID, args.Name, match.JoinOptions{
		PlayerID: args.PlayerID,
	})
	if err != nil {
		return JoinMatchResult{}, err
	}
	return JoinMatchResult{
		MatchID:     args.MatchID,
		PlayerID:    res.PlayerID,
		Seat:        res.Seat,
		Credentials: res.PlayerCredentials,
	}, nil
}

// ----- get_state -----

type GetStateArgs struct {
	MatchID  string `json:"matchID"`
	PlayerID string `json:"playerID,omitempty"`
}

type GetStateResult struct {
	MatchID       string       `json:"matchID"`
	Game          string       `json:"game"`
	State         core.State   `json:"state"`
	CurrentPlayer string       `json:"currentPlayer"`
	Phase         string       `json:"phase,omitempty"`
	Turn          int          `json:"turn"`
	Players       []PlayerInfo `json:"players"`
	Gameover      any          `json:"gameover,omitempty"`
}

type PlayerInfo struct {
	PlayerID    string `json:"playerID"`
	Seat        string `json:"seat"`
	Name        string `json:"name,omitempty"`
	IsConnected bool   `json:"isConnected,omitempty"`
}

// GetState returns the current state as visible to the given playerID. An
// empty playerID gets the spectator view (whatever Game.PlayerView returns
// for "").
func (t *Tools) GetState(_ context.Context, args GetStateArgs) (GetStateResult, error) {
	if args.MatchID == "" {
		return GetStateResult{}, errors.New("matchID is required")
	}
	m, err := t.Manager.State(args.MatchID)
	if err != nil {
		return GetStateResult{}, err
	}
	game := t.Manager.Game(m.GameName)
	if game == nil {
		return GetStateResult{}, fmt.Errorf("game %q is no longer registered", m.GameName)
	}
	state := core.PlayerView(game, m.State, args.PlayerID)
	players := make([]PlayerInfo, 0, len(m.Players))
	for _, p := range m.Players {
		players = append(players, PlayerInfo{
			PlayerID:    p.ID,
			Seat:        p.Seat,
			Name:        p.Name,
			IsConnected: p.IsConnected,
		})
	}
	return GetStateResult{
		MatchID:       m.ID,
		Game:          m.GameName,
		State:         state,
		CurrentPlayer: state.Ctx.CurrentPlayer,
		Phase:         state.Ctx.Phase,
		Turn:          state.Ctx.Turn,
		Players:       players,
		Gameover:      state.Ctx.Gameover,
	}, nil
}

// ----- list_legal_moves -----

type ListLegalMovesArgs struct {
	MatchID  string `json:"matchID"`
	PlayerID string `json:"playerID"`
}

type LegalMove struct {
	Move string `json:"move"`
	Args []any  `json:"args,omitempty"`
}

type ListLegalMovesResult struct {
	MatchID  string      `json:"matchID"`
	PlayerID string      `json:"playerID"`
	Moves    []LegalMove `json:"moves"`
}

// ListLegalMoves enumerates every (move, args) the given player may
// legally play right now. Powered by Game.Enumerate. Games without an
// Enumerate function return an explicit error so the LLM client can
// surface the limitation rather than guessing a move schema.
func (t *Tools) ListLegalMoves(_ context.Context, args ListLegalMovesArgs) (ListLegalMovesResult, error) {
	if args.MatchID == "" {
		return ListLegalMovesResult{}, errors.New("matchID is required")
	}
	if args.PlayerID == "" {
		return ListLegalMovesResult{}, errors.New("playerID is required")
	}
	m, err := t.Manager.State(args.MatchID)
	if err != nil {
		return ListLegalMovesResult{}, err
	}
	game := t.Manager.Game(m.GameName)
	if game == nil {
		return ListLegalMovesResult{}, fmt.Errorf("game %q is no longer registered", m.GameName)
	}
	if game.Enumerate == nil {
		return ListLegalMovesResult{}, fmt.Errorf("game %q does not implement Enumerate; legal-move listing unavailable", m.GameName)
	}
	actions := game.Enumerate(m.State.G, m.State.Ctx, args.PlayerID)
	moves := make([]LegalMove, len(actions))
	for i, a := range actions {
		moves[i] = LegalMove{Move: a.Move, Args: a.Args}
	}
	return ListLegalMovesResult{
		MatchID:  args.MatchID,
		PlayerID: args.PlayerID,
		Moves:    moves,
	}, nil
}

// ----- make_move -----

type MakeMoveArgs struct {
	MatchID     string `json:"matchID"`
	PlayerID    string `json:"playerID"`
	Credentials string `json:"credentials"`
	Move        string `json:"move"`
	Args        []any  `json:"args,omitempty"`
}

type MakeMoveResult struct {
	MatchID       string     `json:"matchID"`
	State         core.State `json:"state"`
	CurrentPlayer string     `json:"currentPlayer"`
	Gameover      any        `json:"gameover,omitempty"`
}

// MakeMove applies a move and returns the resulting state (player-view
// redacted for the mover). When the match ends, Gameover is non-nil.
func (t *Tools) MakeMove(ctx context.Context, args MakeMoveArgs) (MakeMoveResult, error) {
	if args.MatchID == "" {
		return MakeMoveResult{}, errors.New("matchID is required")
	}
	if args.PlayerID == "" {
		return MakeMoveResult{}, errors.New("playerID is required")
	}
	if args.Move == "" {
		return MakeMoveResult{}, errors.New("move is required")
	}
	state, err := t.Manager.MoveReqCtx(ctx, args.MatchID, args.PlayerID, args.Credentials, core.MoveRequest{
		PlayerID: args.PlayerID,
		Move:     args.Move,
		Args:     args.Args,
	})
	if err != nil {
		return MakeMoveResult{}, err
	}
	// Redact the post-move state to the mover's view. One extra storage
	// read to get the game name; cheap with local SQLite or memory.
	if m, err := t.Manager.State(args.MatchID); err == nil {
		if g := t.Manager.Game(m.GameName); g != nil {
			state = core.PlayerView(g, state, args.PlayerID)
		}
	}
	return MakeMoveResult{
		MatchID:       args.MatchID,
		State:         state,
		CurrentPlayer: state.Ctx.CurrentPlayer,
		Gameover:      state.Ctx.Gameover,
	}, nil
}
