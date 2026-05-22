package mcp

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/match"
	"github.com/tjcran/boardgame-go/mcp/modulebridge"
	"github.com/tjcran/boardgame-go/mcp/starlarkgame"
)

// Tools is the set of MCP tool handlers backed by a match.Manager.
//
// Methods are plain Go functions returning JSON-marshallable structs.
// The MCP SDK glue in server.go adapts these to MCP tool registrations;
// keeping the handlers SDK-free lets tests run without any MCP dependency
// and lets us swap the SDK locally if needed.
type Tools struct {
	Manager *match.Manager

	// Registry layers per-user game scoping on top of Manager. When set,
	// list_games / create_match route through it so user-designed games are
	// visible to their owner. When nil, the pre-existing Manager-only path
	// is used (stdio mode, tests without a Registry).
	Registry *UserAwareRegistry

	// Ownership scopes match access by authenticated user. Leave nil for
	// single-tenant mode (stdio, local dev). In hosted/HTTP mode the
	// transport layer sets Ownership and threads the userID through
	// context via WithUserID; create_match claims, every other
	// match-scoped tool verifies via requireOwnership.
	Ownership OwnershipStore
}

// ----- list_games -----

type ListGamesResult struct {
	Games []GameInfo `json:"games"`
}

type GameInfo struct {
	Name       string `json:"name"`
	MinPlayers int    `json:"minPlayers,omitempty"`
	MaxPlayers int    `json:"maxPlayers,omitempty"`
	UserOwned  bool   `json:"userOwned,omitempty"`
}

// ListGames returns metadata for every game visible to the caller.
// When a Registry is configured, per-user games are included for the
// authenticated user; otherwise every built-in game registered on the
// Manager is returned. Sorted by name for deterministic output.
func (t *Tools) ListGames(ctx context.Context) (ListGamesResult, error) {
	if t.Registry != nil {
		userID := UserIDFromContext(ctx)
		listings, err := t.Registry.ListForUser(ctx, userID)
		if err != nil {
			return ListGamesResult{}, err
		}
		out := ListGamesResult{Games: make([]GameInfo, 0, len(listings))}
		for _, l := range listings {
			out.Games = append(out.Games, GameInfo{
				Name:       l.Name,
				MinPlayers: l.MinPlayers,
				MaxPlayers: l.MaxPlayers,
				UserOwned:  l.UserOwned,
			})
		}
		return out, nil
	}
	// Fallback: Manager-only path (stdio mode, tests without Registry).
	// Suppress any usergame-prefixed keys that may have been registered.
	names := t.Manager.GameNames()
	out := ListGamesResult{Games: make([]GameInfo, 0, len(names))}
	for _, name := range names {
		if hasUserGameKeyPrefix(name) {
			continue
		}
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

func (t *Tools) CreateMatch(ctx context.Context, args CreateMatchArgs) (CreateMatchResult, error) {
	if args.Game == "" {
		return CreateMatchResult{}, errors.New("game is required")
	}
	// With ownership scoping on, the caller must have a userID before we
	// create anything — otherwise we'd leak an orphan match into the
	// shared store on auth misconfiguration.
	if t.Ownership != nil && UserIDFromContext(ctx) == "" {
		return CreateMatchResult{}, errors.New("not authenticated: no userID on request context")
	}

	// Translate the public game name to the Manager-internal key. When the
	// Registry is present, user-owned games are scoped by userID. Without a
	// Registry, the public name is used as-is (Manager key == public name for
	// built-ins).
	managerKey := args.Game
	if t.Registry != nil {
		userID := UserIDFromContext(ctx)
		key, _, err := t.Registry.LookupForUser(ctx, userID, args.Game)
		if err != nil {
			return CreateMatchResult{}, fmt.Errorf("game %q: %w", args.Game, err)
		}
		managerKey = key
	}

	matchID, err := t.Manager.Create(managerKey, match.CreateOptions{
		NumPlayers: args.NumPlayers,
		SetupData:  args.SetupData,
		Name:       args.Name,
	})
	if err != nil {
		return CreateMatchResult{}, err
	}
	if t.Ownership != nil {
		if err := t.Ownership.Claim(ctx, UserIDFromContext(ctx), matchID); err != nil {
			return CreateMatchResult{}, fmt.Errorf("claim ownership: %w", err)
		}
	}
	m, err := t.Manager.State(matchID)
	if err != nil {
		return CreateMatchResult{}, err
	}
	return CreateMatchResult{
		MatchID:    matchID,
		Game:       args.Game, // return the public name the caller used
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

func (t *Tools) JoinMatch(ctx context.Context, args JoinMatchArgs) (JoinMatchResult, error) {
	if args.MatchID == "" {
		return JoinMatchResult{}, errors.New("matchID is required")
	}
	if err := t.requireOwnership(ctx, args.MatchID); err != nil {
		return JoinMatchResult{}, err
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
func (t *Tools) GetState(ctx context.Context, args GetStateArgs) (GetStateResult, error) {
	if args.MatchID == "" {
		return GetStateResult{}, errors.New("matchID is required")
	}
	if err := t.requireOwnership(ctx, args.MatchID); err != nil {
		return GetStateResult{}, err
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
		Game:          publicGameName(m.GameName),
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
func (t *Tools) ListLegalMoves(ctx context.Context, args ListLegalMovesArgs) (ListLegalMovesResult, error) {
	if args.MatchID == "" {
		return ListLegalMovesResult{}, errors.New("matchID is required")
	}
	if args.PlayerID == "" {
		return ListLegalMovesResult{}, errors.New("playerID is required")
	}
	if err := t.requireOwnership(ctx, args.MatchID); err != nil {
		return ListLegalMovesResult{}, err
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
	ResumeTag   string `json:"resumeTag,omitempty"`
}

type MakeMoveResult struct {
	MatchID       string     `json:"matchID"`
	State         core.State `json:"state"`
	CurrentPlayer string     `json:"currentPlayer"`
	Gameover      any        `json:"gameover,omitempty"`
}

// ----- register_game -----

type RegisterGameArgs struct {
	Source   string `json:"source"`
	LLMGuide string `json:"llm_guide,omitempty"`
}

type RegisterGameResult struct {
	Name string `json:"name"`
}

// RegisterGame validates a Starlark game spec, persists it under the
// caller's user ID, and installs it on the Manager so it can be played
// like a built-in.
func (t *Tools) RegisterGame(ctx context.Context, args RegisterGameArgs) (RegisterGameResult, error) {
	if t.Registry == nil {
		return RegisterGameResult{}, fmt.Errorf("registry not configured")
	}
	userID := UserIDFromContext(ctx)
	if err := t.Registry.RegisterUserGame(ctx, userID, args.Source, args.LLMGuide); err != nil {
		return RegisterGameResult{}, err
	}
	// Read META back to return the canonical name.
	spec, err := starlarkgame.LoadSpec(args.Source)
	if err != nil {
		return RegisterGameResult{}, err
	}
	return RegisterGameResult{Name: spec.Meta.Name}, nil
}

// ----- playtest_draft -----

type PlaytestStep struct {
	PlayerID string `json:"player_id"`
	Move     string `json:"move"`
	Args     []any  `json:"args,omitempty"`
}

type PlaytestDraftArgs struct {
	Source   string         `json:"source"`
	Scenario []PlaytestStep `json:"scenario,omitempty"`
}

type PlaytestTrace struct {
	PlayerID        string           `json:"player_id"`
	Move            string           `json:"move"`
	Args            []any            `json:"args,omitempty"`
	StateBefore     map[string]any   `json:"state_before"`
	StateAfter      map[string]any   `json:"state_after,omitempty"`
	EndIfResult     any              `json:"end_if_result,omitempty"`
	LegalMovesAfter []map[string]any `json:"legal_moves_after,omitempty"`
	Error           string           `json:"error,omitempty"`
}

type PlaytestDraftResult struct {
	ValidationErrors []string        `json:"validation_errors,omitempty"`
	SetupState       map[string]any  `json:"setup_state,omitempty"`
	Trace            []PlaytestTrace `json:"trace,omitempty"`
}

// PlaytestDraft dry-runs a draft spec without registering it. It returns
// any validation errors, the initial state, and a per-step trace for the
// optional scenario. Side-effect-free; no DB write.
func (t *Tools) PlaytestDraft(ctx context.Context, args PlaytestDraftArgs) (PlaytestDraftResult, error) {
	var res PlaytestDraftResult
	spec, err := starlarkgame.LoadSpec(args.Source)
	if err != nil {
		res.ValidationErrors = []string{"parse: " + err.Error()}
		return res, nil
	}
	if err := starlarkgame.Validate(ctx, spec); err != nil {
		res.ValidationErrors = []string{"validate: " + err.Error()}
		return res, nil
	}
	// Instantiate declared modules so setup/move callbacks can use
	// ctx.modules.<name>.<op>(...), exactly as a live match does.
	mods := spec.NewModuleStates()
	bc := starlarkgame.NewWriteCtx(spec.Meta.MinPlayers, "", mods)
	bc.AttachSeededRandom(0)
	state, err := spec.CallSetup(ctx, bc)
	if err != nil {
		return res, err
	}
	res.SetupState = state

	for _, step := range args.Scenario {
		bc.PlayerID = step.PlayerID
		tr := PlaytestTrace{
			PlayerID:    step.PlayerID,
			Move:        step.Move,
			Args:        step.Args,
			StateBefore: deepCopyMap(state),
		}
		next, err := spec.CallMove(ctx, bc, step.Move, state, step.Args)
		if err != nil {
			tr.Error = err.Error()
			res.Trace = append(res.Trace, tr)
			break
		}
		state = next
		tr.StateAfter = deepCopyMap(state)
		// end_if / legal_moves are speculative reads: mirror the live
		// engine's read-only module contract over the same module states.
		roBC := starlarkgame.NewReadCtx(spec.Meta.MinPlayers, step.PlayerID, mods)
		roBC.AttachSeededRandom(0)
		if end, _ := spec.CallEndIf(ctx, roBC, state); end != nil {
			tr.EndIfResult = end
		}
		lm, _ := spec.CallLegalMoves(ctx, roBC, state)
		tr.LegalMovesAfter = lm
		res.Trace = append(res.Trace, tr)
	}
	return res, nil
}

// ----- delete_game -----

type DeleteGameArgs struct {
	Name string `json:"name"`
}
type DeleteGameResult struct {
	Deleted bool `json:"deleted"`
}

// DeleteGame removes a user-designed game. Built-ins are protected by
// UserAwareRegistry.DeleteUserGame (which only knows about user games).
func (t *Tools) DeleteGame(ctx context.Context, args DeleteGameArgs) (DeleteGameResult, error) {
	if t.Registry == nil {
		return DeleteGameResult{}, fmt.Errorf("registry not configured")
	}
	userID := UserIDFromContext(ctx)
	if err := t.Registry.DeleteUserGame(ctx, userID, args.Name); err != nil {
		return DeleteGameResult{}, err
	}
	return DeleteGameResult{Deleted: true}, nil
}

// ExportGameArgs / ExportGameResult / ExportGameMove are the wire types
// for export_game. The result is structured (no archive packaging) so
// callers can compose whatever distribution format they want — write
// the SKILL.md and spec.star to a directory, tar it, post it, etc.
type ExportGameArgs struct {
	Name string `json:"name"`
}

type ExportGameMove struct {
	Name string                  `json:"name"`
	Args []starlarkgame.ArgDef   `json:"args,omitempty"`
}

type ExportGameManifest struct {
	Name        string           `json:"name"`
	Description string           `json:"description,omitempty"`
	Owner       string           `json:"owner,omitempty"`
	MinPlayers  int              `json:"min_players"`
	MaxPlayers  int              `json:"max_players"`
	CreatedAt   string           `json:"created_at,omitempty"`
	Moves       []ExportGameMove `json:"moves"`
}

type ExportGameResult struct {
	Name     string             `json:"name"`
	SkillMD  string             `json:"skill_md"`
	SpecStar string             `json:"spec_star"`
	Manifest ExportGameManifest `json:"manifest"`
}

// ExportGame returns a skill-shaped package for a designed game owned
// by the caller. The SKILL.md is a skeleton — frontmatter, an auto-
// rendered moves table, the designer's llm_guide — meant as a starting
// point for the game's per-game Claude skill. Strategy prose, UI notes,
// and AI heuristics are the author's job to add.
//
// Built-ins can't be exported (no spec source to round-trip). Cross-
// owner exports are refused at the registry level.
func (t *Tools) ExportGame(ctx context.Context, args ExportGameArgs) (ExportGameResult, error) {
	if t.Registry == nil {
		return ExportGameResult{}, fmt.Errorf("registry not configured")
	}
	if args.Name == "" {
		return ExportGameResult{}, fmt.Errorf("export_game: name is required")
	}
	userID := UserIDFromContext(ctx)

	ug, err := t.Registry.UserGame(ctx, userID, args.Name)
	if err != nil {
		return ExportGameResult{}, err
	}
	if ug == nil {
		return ExportGameResult{}, fmt.Errorf("export_game: %q is not a game you own (built-ins can't be exported)", args.Name)
	}

	spec, err := starlarkgame.LoadSpec(ug.Source)
	if err != nil {
		return ExportGameResult{}, fmt.Errorf("export_game: stored spec failed to parse: %w", err)
	}

	skeleton := starlarkgame.BuildSkillSkeleton(spec, ug.LLMGuide, ug.UserID, ug.CreatedAt)

	moves := make([]ExportGameMove, 0, len(skeleton.Moves))
	for _, m := range skeleton.Moves {
		moves = append(moves, ExportGameMove{Name: m.Name, Args: m.Args})
	}
	manifest := ExportGameManifest{
		Name:        skeleton.Name,
		Description: skeleton.Description,
		Owner:       skeleton.Owner,
		MinPlayers:  skeleton.MinPlayers,
		MaxPlayers:  skeleton.MaxPlayers,
		Moves:       moves,
	}
	if !skeleton.CreatedAt.IsZero() {
		manifest.CreatedAt = skeleton.CreatedAt.UTC().Format(time.RFC3339)
	}

	return ExportGameResult{
		Name:     skeleton.Name,
		SkillMD:  skeleton.RenderMarkdown(),
		SpecStar: ug.Source,
		Manifest: manifest,
	}, nil
}

// deepCopyMap shallow-copies the top level; nested values are shared.
// Sufficient for the trace, which is reported once per step.
func deepCopyMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
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
	if err := t.requireOwnership(ctx, args.MatchID); err != nil {
		return MakeMoveResult{}, err
	}
	state, err := t.Manager.MoveReqCtx(ctx, args.MatchID, args.PlayerID, args.Credentials, core.MoveRequest{
		PlayerID:  args.PlayerID,
		Move:      args.Move,
		Args:      args.Args,
		ResumeTag: args.ResumeTag,
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

// ----- module_op -----

// ModuleOpArgs invokes one engine-module op against a draft match's live
// module state. Design-time only: lets Claude prototype mechanics on a
// draft game interactively. Match-scoped + ownership-checked.
type ModuleOpArgs struct {
	MatchID string         `json:"matchId"`
	Module  string         `json:"module"`
	Op      string         `json:"op"`
	Args    map[string]any `json:"args,omitempty"`
}

type ModuleOpResult struct {
	Result any `json:"result,omitempty"`
}

// ModuleOp resolves the live module state from the named match's
// *starlarkgame.StarlarkG and invokes the exact same modulebridge Op.Call
// the Starlark binding uses, guaranteeing parity between design-time pokes
// and in-game behavior.
func (t *Tools) ModuleOp(ctx context.Context, args ModuleOpArgs) (ModuleOpResult, error) {
	if err := t.requireOwnership(ctx, args.MatchID); err != nil {
		return ModuleOpResult{}, err
	}
	reg := modulebridge.RegistryFor(args.Module)
	if reg == nil {
		return ModuleOpResult{}, fmt.Errorf("unknown module %q", args.Module)
	}
	var chosen *modulebridge.Op
	for _, op := range reg.Ops(args.Module) {
		if op.Name == args.Op {
			op := op
			chosen = &op
			break
		}
	}
	if chosen == nil {
		return ModuleOpResult{}, fmt.Errorf("unknown op %q for module %q", args.Op, args.Module)
	}

	m, err := t.Manager.State(args.MatchID)
	if err != nil {
		return ModuleOpResult{}, err
	}
	sg, ok := m.State.G.(*starlarkgame.StarlarkG)
	if !ok {
		return ModuleOpResult{}, fmt.Errorf("match %s is not a designed game", args.MatchID)
	}
	if _, ok := sg.Modules[args.Module]; !ok {
		return ModuleOpResult{}, fmt.Errorf("match %s did not declare module %q", args.MatchID, args.Module)
	}
	res, err := chosen.Call(sg.Modules, args.Args, core.NewRandomFromState(new(uint64)))
	if err != nil {
		return ModuleOpResult{}, err
	}
	return ModuleOpResult{Result: res}, nil
}

// ----- describe_modules -----

// DescribeModulesArgs optionally filters discovery to a single module.
type DescribeModulesArgs struct {
	Module string `json:"module,omitempty"`
}

// OpInfo names one invokable op. MCPTool is the flattened tool alias the
// Starlark binding also exposes it under. ReadOnly marks a pure query that
// is safe to call from read-only callbacks (legal_moves/end_if/player_view).
type OpInfo struct {
	Name     string `json:"name"`
	MCPTool  string `json:"mcpTool,omitempty"`
	ReadOnly bool   `json:"readOnly"`
}

// ModuleInfo is one module and its ops.
type ModuleInfo struct {
	Module string   `json:"module"`
	Ops    []OpInfo `json:"ops"`
}

type DescribeModulesResult struct {
	Modules []ModuleInfo `json:"modules"`
}

// DescribeModules enumerates the engine modules reachable via module_op and
// the op names each exposes. Static metadata: needs no match and mutates
// nothing, so an agent can discover the surface before creating a draft.
func (t *Tools) DescribeModules(ctx context.Context, args DescribeModulesArgs) (DescribeModulesResult, error) {
	names := modulebridge.AllModules()
	if args.Module != "" {
		if modulebridge.RegistryFor(args.Module) == nil {
			return DescribeModulesResult{}, fmt.Errorf("unknown module %q", args.Module)
		}
		names = []string{args.Module}
	}

	out := make([]ModuleInfo, 0, len(names))
	for _, name := range names {
		reg := modulebridge.RegistryFor(name)
		if reg == nil {
			continue
		}
		ops := make([]OpInfo, 0)
		for _, op := range reg.Ops(name) {
			ops = append(ops, OpInfo{Name: op.Name, MCPTool: op.MCPTool, ReadOnly: op.ReadOnly})
		}
		sort.Slice(ops, func(i, j int) bool { return ops[i].Name < ops[j].Name })
		out = append(out, ModuleInfo{Module: name, Ops: ops})
	}
	return DescribeModulesResult{Modules: out}, nil
}
