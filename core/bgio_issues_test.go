package core

import (
	"testing"
)

// TestBGIO1237_EndTurnInPhaseOnBegin reproduces BGIO issue #1237: calling
// events.EndTurn() inside a phase's OnBegin hook was silently discarded.
// Our engine drains hook events through the surrounding loop so this works.
//
// The reproduction uses no MinMoves (matching the original report). With
// MinMoves > 0, the engine intentionally blocks any EndTurn — including
// engine-driven ones — until the floor is reached.
func TestBGIO1237_EndTurnInPhaseOnBegin(t *testing.T) {
	g := &Game{
		Name:       "endturn-in-onbegin",
		MinPlayers: 3,
		MaxPlayers: 3,
		Setup:      func(_ Ctx, _ any) G { return struct{}{} },
		Phases: map[string]*PhaseConfig{
			"main": {
				Start: true,
				OnBegin: func(mc *MoveContext) G {
					mc.Events.EndTurn("2") // skip to player 2
					return mc.G
				},
				Moves: map[string]any{"noop": noopMove},
			},
		},
	}
	s := NewMatch(g, 0, nil)
	if s.Ctx.CurrentPlayer != "2" {
		t.Fatalf("expected current=2 after phase.OnBegin events.EndTurn, got %s",
			s.Ctx.CurrentPlayer)
	}
}

// TestBGIO608_StageOnBeginOnEnd verifies that the OnBegin/OnEnd hooks BGIO
// never landed work in this port (issue #608).
func TestBGIO608_StageOnBeginOnEnd(t *testing.T) {
	var trace []string
	g := &Game{
		Name:       "stage-hooks",
		MinPlayers: 2,
		MaxPlayers: 2,
		Setup:      func(_ Ctx, _ any) G { return struct{}{} },
		Moves: map[string]any{
			"enter": MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
				mc.Events.SetStage("discard")
				return mc.G, nil
			}),
			"leave": MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
				mc.Events.EndStage()
				return mc.G, nil
			}),
		},
		Turn: &TurnConfig{
			Stages: map[string]*StageConfig{
				"discard": {
					OnBegin: func(mc *MoveContext) G {
						trace = append(trace, "begin:"+mc.PlayerID)
						return mc.G
					},
					OnEnd: func(mc *MoveContext) G {
						trace = append(trace, "end:"+mc.PlayerID)
						return mc.G
					},
					Moves: map[string]any{"leave": MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
						mc.Events.EndStage()
						return mc.G, nil
					})},
				},
			},
		},
	}
	s := NewMatch(g, 0, nil)
	s, _ = Apply(g, s, MoveRequest{PlayerID: "0", Move: "enter"})
	if len(trace) != 1 || trace[0] != "begin:0" {
		t.Fatalf("expected stage.OnBegin to fire for player 0, trace=%v", trace)
	}
	s, _ = Apply(g, s, MoveRequest{PlayerID: "0", Move: "leave"})
	if len(trace) != 2 || trace[1] != "end:0" {
		t.Fatalf("expected stage.OnEnd to fire for player 0, trace=%v", trace)
	}
}

// TestBGIO1228_HookLogEntries verifies mc.AddLog from inside a hook lands
// in the persisted log — BGIO's log plugin only sees move-time updates.
func TestBGIO1228_HookLogEntries(t *testing.T) {
	g := &Game{
		Name:       "hook-log",
		MinPlayers: 2,
		MaxPlayers: 2,
		Setup:      func(_ Ctx, _ any) G { return struct{}{} },
		Moves: map[string]any{
			"act": MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
				mc.AddLog(LogEntry{Kind: "domain", Move: "fanfare"})
				return mc.G, nil
			}),
		},
		Turn: &TurnConfig{
			OnBegin: func(mc *MoveContext) G {
				mc.AddLog(LogEntry{Kind: "domain", Move: "turn-start"})
				return mc.G
			},
			MinMoves: 1, MaxMoves: 1,
		},
	}
	s := NewMatch(g, 0, nil)
	s, err := Apply(g, s, MoveRequest{PlayerID: "0", Move: "act"})
	if err != nil {
		t.Fatalf("act: %v", err)
	}
	// Expect: move-kind log (from the move), domain "fanfare" (from move's
	// AddLog), then domain "turn-start" (from the next turn's OnBegin
	// running after auto-end-turn).
	hasFanfare, hasStart := false, false
	for _, e := range s.Log {
		if e.Kind == "domain" && e.Move == "fanfare" {
			hasFanfare = true
		}
		if e.Kind == "domain" && e.Move == "turn-start" {
			hasStart = true
		}
	}
	if !hasFanfare || !hasStart {
		t.Fatalf("missing custom log entries (fanfare=%v turnstart=%v): %+v",
			hasFanfare, hasStart, s.Log)
	}
}

// TestBGIO777_ActivePlayersSerialisesAsEmptyObject verifies the marshalling
// fix: nil ActivePlayers becomes `{}` on the wire so clients don't need a
// null guard before `in` checks.
func TestBGIO777_ActivePlayersSerialisesAsEmptyObject(t *testing.T) {
	c := Ctx{} // ActivePlayers is nil
	b, err := c.MarshalJSON()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !contains777(string(b), `"activePlayers":{}`) {
		t.Fatalf("expected activePlayers={}, got %s", string(b))
	}
}

func contains777(s, needle string) bool {
	for i := 0; i+len(needle) <= len(s); i++ {
		if s[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

// TestBGIO1135_OnUndoIntercepts verifies Game.OnUndo fires after undo
// restores the prior state, so games can scrub transient (animation /
// hint) fields.
func TestBGIO1135_OnUndoIntercepts(t *testing.T) {
	type s struct {
		Score    int
		LastAnim string
	}
	g := &Game{
		Name:       "undo-intercept",
		MinPlayers: 1,
		MaxPlayers: 1,
		Setup:      func(_ Ctx, _ any) G { return &s{} },
		Moves: map[string]any{
			"inc": Move{
				Move: func(mc *MoveContext, _ ...any) (G, error) {
					st := mc.G.(*s)
					return &s{Score: st.Score + 1, LastAnim: "ping"}, nil
				},
				Undoable: true,
			},
		},
		OnUndo: func(mc *MoveContext) G {
			st := mc.G.(*s)
			// Scrub the animation field on undo.
			out := *st
			out.LastAnim = ""
			return &out
		},
		Turn: &TurnConfig{}, // multi-move turn for undo
	}
	st := NewMatch(g, 0, nil)
	st, _ = Apply(g, st, MoveRequest{PlayerID: "0", Move: "inc"})
	st, _ = Apply(g, st, MoveRequest{PlayerID: "0", Move: "inc"})
	if st.G.(*s).LastAnim != "ping" {
		t.Fatal("expected animation set after move")
	}
	u, err := Undo(g, st)
	if err != nil {
		t.Fatalf("undo: %v", err)
	}
	if u.G.(*s).LastAnim != "" {
		t.Fatalf("expected OnUndo to scrub LastAnim, got %q", u.G.(*s).LastAnim)
	}
}

// TestBGIO616_PlayerElimination verifies events.RemovePlayer eliminates a
// player from turn order (BGIO #616).
func TestBGIO616_PlayerElimination(t *testing.T) {
	g := &Game{
		Name:       "eliminate",
		MinPlayers: 3,
		MaxPlayers: 3,
		Setup:      func(_ Ctx, _ any) G { return struct{}{} },
		Moves: map[string]any{
			"eliminateP1": MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
				mc.Events.RemovePlayer("1")
				return mc.G, nil
			}),
			"noop": noopMove,
		},
		Turn: &TurnConfig{MinMoves: 1, MaxMoves: 1},
	}
	s := NewMatch(g, 0, nil)
	s, err := Apply(g, s, MoveRequest{PlayerID: "0", Move: "eliminateP1"})
	if err != nil {
		t.Fatalf("eliminate: %v", err)
	}
	for _, p := range s.Ctx.PlayOrder {
		if p == "1" {
			t.Fatalf("player 1 should be removed from PlayOrder, got %v", s.Ctx.PlayOrder)
		}
	}
	// Next current player should be "2", not "1" (the auto-end-turn after
	// the move skipped seat 1).
	if s.Ctx.CurrentPlayer != "2" {
		t.Fatalf("expected current=2 after eliminating 1, got %s", s.Ctx.CurrentPlayer)
	}
}
