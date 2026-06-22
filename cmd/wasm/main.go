//go:build js && wasm

// Command wasm is the Go WebAssembly entrypoint for boardgame-go. It binds
// the core engine + bots packages into the browser via syscall/js and
// exposes a single global JS function, `simulateBattle`, that runs an
// in-memory bot-vs-bot battle and returns a deterministic, seed-driven
// battle log.
//
// Design note: bots.Simulate (bots/simulate.go) already runs matches purely
// in-memory with no Manager/storage/network I/O, but it only returns the
// aggregate SimulationResult and discards the per-move history. The browser
// demo wants to *show* the battle, so this entrypoint mirrors Simulate's
// inner loop (core.NewMatchSeeded → bot.Play → core.Apply, capped by
// MaxMoves, honouring ActivePlayers) while additionally recording every move
// into a structured log. Everything is seeded so the same seed always
// produces byte-identical output (see JS_LIMITATIONS.md §3 on deterministic
// replay).
//
// Build with:
//
//	GOOS=js GOARCH=wasm go build -o web/main.wasm ./cmd/wasm
//
// then serve web/ (index.html, wasm_exec.js, app.js, main.wasm) statically.
package main

import (
	"context"
	"fmt"
	"strconv"

	"github.com/tjcran/boardgame-go/bots"
	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/games/tictactoe"

	"syscall/js"
)

func main() {
	// Register the JS bridge, then block forever: a wasm "main" that
	// returns tears down the Go runtime and unregisters every callback,
	// so we park on an empty channel to keep simulateBattle alive.
	js.Global().Set("simulateBattle", js.FuncOf(simulateBattle))
	js.Global().Set("boardgameGoReady", js.ValueOf(true))
	select {}
}

// battleEntry is one recorded ply in the deterministic log.
type battleEntry struct {
	Step   int    // 1-based move index within the match
	Turn   int    // ctx.Turn at the time the move was applied
	Player string // seat that moved
	Move   string // move name (e.g. "clickCell")
	Args   []any  // move arguments
	Board  string // post-move board rendering (tic-tac-toe specific)
}

// simulateBattle is the JS-facing entrypoint:
//
//	simulateBattle(seed)                  // seed only
//	simulateBattle(seed, matches)         // + match count
//	simulateBattle(seed, matches, maxMoves)
//
// `seed` may be a JS number or string; either is fed into the engine and the
// bots so the run is fully deterministic. Returns a JS object:
//
//	{
//	  ok: true,
//	  seed: "...",
//	  matches: 1,
//	  result: { matches, wins:{...}, draws, errors, avgMoves, avgTurns },
//	  log: [ "...", ... ],            // human-readable lines for the first match
//	  entries: [ {step,turn,player,move,args,board}, ... ],
//	  outcome: "winner: 0" | "draw" | "incomplete",
//	}
//
// On bad input it returns { ok:false, error:"..." } instead of throwing, so
// app.js can render the error inline.
func simulateBattle(_ js.Value, args []js.Value) any {
	if len(args) < 1 {
		return errObject("simulateBattle requires at least a seed argument")
	}

	seed := seedFromJS(args[0])

	matches := 1
	if len(args) >= 2 && args[1].Type() == js.TypeNumber {
		if n := args[1].Int(); n > 0 {
			matches = n
		}
	}
	maxMoves := 1000
	if len(args) >= 3 && args[2].Type() == js.TypeNumber {
		if n := args[2].Int(); n > 0 {
			maxMoves = n
		}
	}

	game := tictactoe.New()

	// Aggregate stats, mirroring bots.SimulationResult so the JS side gets
	// the same shape it would from bots.Simulate.
	result := bots.SimulationResult{Wins: map[string]int{}}
	var totalMoves, totalTurns, completed int

	// We keep the structured log + readable lines for the FIRST match only
	// (the demo renders one battle); later matches still feed the aggregate.
	var firstEntries []battleEntry
	var firstLines []string
	firstOutcome := "incomplete"

	ctx := context.Background()

	for m := 0; m < matches; m++ {
		// Per-match seed: combine the base seed with the match index so
		// each match in a multi-match run is distinct yet reproducible.
		matchSeed := seed + uint64(m)

		// Two RandomBots sharing the match seed. RandomBot is deterministic
		// given a Seed (bots/bots.go), so the whole battle replays identically.
		botX := &bots.RandomBot{Game: game, Seed: matchSeed}
		botO := &bots.RandomBot{Game: game, Seed: matchSeed ^ 0x5555555555555555}
		seats := map[string]bots.Bot{"0": botX, "1": botO}

		state := core.NewMatchSeeded(game, 0, nil, matchSeed)

		var entries []battleEntry
		var lines []string
		recordFirst := m == 0

		moves := 0
		errored := false
		for moves < maxMoves && state.Ctx.Gameover == nil {
			pid := state.Ctx.CurrentPlayer
			// Honour active-players, exactly like bots.Simulate.
			if state.Ctx.ActivePlayers != nil {
				for k := range state.Ctx.ActivePlayers {
					pid = k
					break
				}
			}
			bot, ok := seats[pid]
			if !ok {
				errored = true
				break
			}
			action, err := bot.Play(ctx, state, pid)
			if err != nil {
				errored = true
				break
			}
			next, err := core.Apply(game, state, core.MoveRequest{
				PlayerID: pid, Move: action.Move, Args: action.Args,
			})
			if err != nil {
				errored = true
				break
			}
			state = next
			moves++

			if recordFirst {
				e := battleEntry{
					Step:   moves,
					Turn:   state.Ctx.Turn,
					Player: pid,
					Move:   action.Move,
					Args:   action.Args,
					Board:  renderBoard(state),
				}
				entries = append(entries, e)
				lines = append(lines, fmt.Sprintf(
					"step %d (turn %d): player %s -> %s%s",
					e.Step, e.Turn, e.Player, e.Move, formatArgs(e.Args),
				))
			}
		}

		result.Matches++
		if errored || state.Ctx.Gameover == nil {
			result.Errors++
			if recordFirst {
				firstOutcome = "incomplete"
			}
		} else {
			completed++
			totalMoves += moves
			totalTurns += state.Ctx.Turn
			if g, ok := state.Ctx.Gameover.(map[string]any); ok {
				if w, ok := g["winner"].(string); ok {
					result.Wins[w]++
					if recordFirst {
						firstOutcome = "winner: " + w
					}
				} else if d, _ := g["draw"].(bool); d {
					result.Draws++
					if recordFirst {
						firstOutcome = "draw"
					}
				}
			}
		}

		if recordFirst {
			lines = append(lines, "result: "+firstOutcome)
			firstEntries = entries
			firstLines = lines
		}
	}

	if completed > 0 {
		result.AvgMoves = float64(totalMoves) / float64(completed)
		result.AvgTurns = float64(totalTurns) / float64(completed)
	}

	return js.ValueOf(map[string]any{
		"ok":      true,
		"seed":    strconv.FormatUint(seed, 10),
		"matches": matches,
		"result":  resultToJS(result),
		"log":     stringsToJS(firstLines),
		"entries": entriesToJS(firstEntries),
		"outcome": firstOutcome,
	})
}

// seedFromJS turns the first JS argument into a uint64 seed. Numbers are
// truncated to their integer value; strings are parsed as base-10 when
// possible and otherwise FNV-hashed so any text yields a stable seed.
func seedFromJS(v js.Value) uint64 {
	switch v.Type() {
	case js.TypeNumber:
		return uint64(int64(v.Float()))
	case js.TypeString:
		s := v.String()
		if n, err := strconv.ParseUint(s, 10, 64); err == nil {
			return n
		}
		return fnv1a(s)
	default:
		return 0
	}
}

// fnv1a is the same FNV-1a hash bots.seedToUint64 uses for string seeds, so
// a string seed here maps consistently with the bots package convention.
func fnv1a(s string) uint64 {
	var h uint64 = 14695981039346656037
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= 1099511628211
	}
	return h
}

// renderBoard draws the tic-tac-toe board after a move, e.g. "X.O|.X.|..O".
func renderBoard(state core.State) string {
	s, ok := state.G.(*tictactoe.State)
	if !ok {
		return ""
	}
	out := make([]byte, 0, 11)
	for i, c := range s.Cells {
		if i > 0 && i%3 == 0 {
			out = append(out, '|')
		}
		if c == "" {
			out = append(out, '.')
		} else {
			out = append(out, c[0])
		}
	}
	return string(out)
}

func formatArgs(args []any) string {
	if len(args) == 0 {
		return ""
	}
	return fmt.Sprintf(" %v", args)
}

func resultToJS(r bots.SimulationResult) any {
	wins := map[string]any{}
	for k, v := range r.Wins {
		wins[k] = v
	}
	return map[string]any{
		"matches":  r.Matches,
		"wins":     wins,
		"draws":    r.Draws,
		"errors":   r.Errors,
		"avgMoves": r.AvgMoves,
		"avgTurns": r.AvgTurns,
	}
}

func entriesToJS(entries []battleEntry) any {
	out := make([]any, 0, len(entries))
	for _, e := range entries {
		out = append(out, map[string]any{
			"step":   e.Step,
			"turn":   e.Turn,
			"player": e.Player,
			"move":   e.Move,
			"args":   argsToJS(e.Args),
			"board":  e.Board,
		})
	}
	return out
}

func argsToJS(args []any) any {
	out := make([]any, 0, len(args))
	for _, a := range args {
		switch v := a.(type) {
		case int:
			out = append(out, v)
		case int64:
			out = append(out, int(v))
		case float64:
			out = append(out, v)
		case string:
			out = append(out, v)
		case bool:
			out = append(out, v)
		default:
			out = append(out, fmt.Sprintf("%v", v))
		}
	}
	return out
}

func stringsToJS(ss []string) any {
	out := make([]any, 0, len(ss))
	for _, s := range ss {
		out = append(out, s)
	}
	return out
}

func errObject(msg string) any {
	return js.ValueOf(map[string]any{
		"ok":    false,
		"error": msg,
	})
}
