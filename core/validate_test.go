package core

import (
	"strings"
	"testing"
)

func TestValidateRejectsNilSetup(t *testing.T) {
	g := &Game{Name: "x"}
	err := g.Validate()
	if err == nil || !strings.Contains(err.Error(), "Setup is nil") {
		t.Fatalf("expected Setup-is-nil error, got %v", err)
	}
}

func TestValidateRejectsMultipleStartPhases(t *testing.T) {
	g := &Game{
		Name:  "x",
		Setup: func(_ Ctx, _ any) G { return struct{}{} },
		Phases: map[string]*PhaseConfig{
			"a": {Start: true},
			"b": {Start: true},
		},
	}
	err := g.Validate()
	if err == nil || !strings.Contains(err.Error(), "Start=true") {
		t.Fatalf("expected multiple-start error, got %v", err)
	}
}

func TestValidateRejectsDanglingPhaseNext(t *testing.T) {
	g := &Game{
		Name:  "x",
		Setup: func(_ Ctx, _ any) G { return struct{}{} },
		Phases: map[string]*PhaseConfig{
			"a": {Next: "ghost", Start: true},
		},
	}
	err := g.Validate()
	if err == nil || !strings.Contains(err.Error(), "ghost") {
		t.Fatalf("expected dangling Next error, got %v", err)
	}
}

func TestValidateRejectsMinGreaterThanMax(t *testing.T) {
	g := &Game{
		Name:       "x",
		Setup:      func(_ Ctx, _ any) G { return struct{}{} },
		MinPlayers: 5,
		MaxPlayers: 2,
	}
	err := g.Validate()
	if err == nil || !strings.Contains(err.Error(), "MinPlayers") {
		t.Fatalf("expected min>max error, got %v", err)
	}
}

func TestValidateRejectsBadMoveType(t *testing.T) {
	g := &Game{
		Name:  "x",
		Setup: func(_ Ctx, _ any) G { return struct{}{} },
		Moves: map[string]any{"bad": "not a move"},
	}
	err := g.Validate()
	if err == nil || !strings.Contains(err.Error(), "move") {
		t.Fatalf("expected bad-move-type error, got %v", err)
	}
}

func TestValidateAcceptsWellFormedGame(t *testing.T) {
	g := &Game{
		Name:       "x",
		MinPlayers: 2,
		MaxPlayers: 2,
		Setup:      func(_ Ctx, _ any) G { return struct{}{} },
		Moves:      map[string]any{"x": noopMove},
		Phases: map[string]*PhaseConfig{
			"a": {Start: true, Next: "b"},
			"b": {},
		},
	}
	if err := g.Validate(); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}
