package core

import (
	"encoding/json"
	"errors"
	"testing"
)

// targetState records each move along with any selection the resume
// move pulled from mc.ResumingBlock. Used as the G across target
// tests.
type targetState struct {
	Events    []string
	Selection []any
	SawResume bool
}

func cloneT(s *targetState, more ...string) *targetState {
	out := &targetState{
		Events:    append([]string(nil), s.Events...),
		Selection: append([]any(nil), s.Selection...),
		SawResume: s.SawResume,
	}
	out.Events = append(out.Events, more...)
	return out
}

// targetGame builds a game with one "askForTarget" move that emits a
// RequestTarget for candidate IDs [10, 20, 30] (Min=1, Max=1), and a
// "pickTarget" resume move that reads mc.ResumingBlock.Target and
// validates the selection.
func targetGame() *Game {
	return &Game{
		Name:       "target-test",
		MinPlayers: 1,
		MaxPlayers: 1,
		Setup:      func(_ Ctx, _ any) G { return &targetState{} },
		Moves: map[string]any{
			"askForTarget": MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
				mc.Queue.RequestTarget(mc.PlayerID, TargetRequest{
					Kind:       "damage",
					Candidates: []any{10, 20, 30},
					Min:        1,
					Max:        1,
					Source:     "spell-A",
				})
				return cloneT(mc.G.(*targetState), "askForTarget"), nil
			}),
			"pickTarget": MoveFn(func(mc *MoveContext, args ...any) (G, error) {
				g := cloneT(mc.G.(*targetState), "pickTarget")
				if mc.ResumingBlock == nil || mc.ResumingBlock.Target == nil {
					return g, errors.New("pickTarget expected a ResumingBlock with Target")
				}
				g.SawResume = true
				req := *mc.ResumingBlock.Target
				if err := ValidateSelection(req, args); err != nil {
					return mc.G, err
				}
				g.Selection = append([]any(nil), args...)
				return g, nil
			}),
			// Non-target pause for the "old Block API still works" test.
			"plainBlock": MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
				mc.Queue.Block("confirm", mc.PlayerID, "raw-data")
				return cloneT(mc.G.(*targetState), "plainBlock"), nil
			}),
			"confirm": MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
				g := cloneT(mc.G.(*targetState), "confirm")
				if mc.ResumingBlock == nil {
					return g, errors.New("confirm expected ResumingBlock")
				}
				if mc.ResumingBlock.Target != nil {
					return g, errors.New("confirm expected nil Target on plain block")
				}
				if mc.ResumingBlock.Data != "raw-data" {
					return g, errors.New("confirm expected Data='raw-data'")
				}
				return g, nil
			}),
		},
	}
}

// TestRequestTargetCreatesTypedBlock verifies the move's RequestTarget
// call lands as a BlockSpec on State.Blocks with the typed Target
// payload intact.
func TestRequestTargetCreatesTypedBlock(t *testing.T) {
	g := targetGame()
	s := NewMatch(g, 0, nil)
	s, err := Apply(g, s, MoveRequest{PlayerID: "0", Move: "askForTarget"})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(s.Blocks) != 1 {
		t.Fatalf("expected 1 pending block, got %d", len(s.Blocks))
	}
	b := s.Blocks[0]
	if b.Tag != "damage" {
		t.Fatalf("expected Tag=damage (mirrors Kind), got %q", b.Tag)
	}
	if b.Target == nil {
		t.Fatalf("expected typed Target on block")
	}
	if b.Target.Kind != "damage" || b.Target.Min != 1 || b.Target.Max != 1 ||
		len(b.Target.Candidates) != 3 || b.Target.Source != "spell-A" {
		t.Fatalf("unexpected Target payload: %+v", b.Target)
	}
}

// TestResumingBlockSurfacesTypedTarget runs the full ask -> pause ->
// resume flow and verifies the resume move sees mc.ResumingBlock with
// the typed Target.
func TestResumingBlockSurfacesTypedTarget(t *testing.T) {
	g := targetGame()
	s := NewMatch(g, 0, nil)
	s, err := Apply(g, s, MoveRequest{PlayerID: "0", Move: "askForTarget"})
	if err != nil {
		t.Fatalf("ask: %v", err)
	}
	s, err = Apply(g, s, MoveRequest{
		PlayerID: "0", Move: "pickTarget", ResumeTag: "damage", Args: []any{20},
	})
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	ts := s.G.(*targetState)
	if !ts.SawResume {
		t.Fatalf("resume move did not observe ResumingBlock")
	}
	if len(ts.Selection) != 1 || ts.Selection[0] != 20 {
		t.Fatalf("expected Selection=[20], got %v", ts.Selection)
	}
	if len(s.Blocks) != 0 {
		t.Fatalf("expected block consumed, got %+v", s.Blocks)
	}
}

// TestResumeRejectsBadCardinality verifies the resume move's
// ValidateSelection rejects too-many / too-few picks.
func TestResumeRejectsBadCardinality(t *testing.T) {
	g := targetGame()
	s := NewMatch(g, 0, nil)
	s, err := Apply(g, s, MoveRequest{PlayerID: "0", Move: "askForTarget"})
	if err != nil {
		t.Fatalf("ask: %v", err)
	}
	// Two picks when Max=1.
	_, err = Apply(g, s, MoveRequest{
		PlayerID: "0", Move: "pickTarget", ResumeTag: "damage", Args: []any{10, 20},
	})
	if !errors.Is(err, ErrTargetCardinality) {
		t.Fatalf("expected ErrTargetCardinality for over-pick, got %v", err)
	}
	// Zero picks.
	_, err = Apply(g, s, MoveRequest{
		PlayerID: "0", Move: "pickTarget", ResumeTag: "damage", Args: nil,
	})
	if !errors.Is(err, ErrTargetCardinality) {
		t.Fatalf("expected ErrTargetCardinality for under-pick, got %v", err)
	}
}

// TestResumeRejectsNonCandidate verifies membership enforcement.
func TestResumeRejectsNonCandidate(t *testing.T) {
	g := targetGame()
	s := NewMatch(g, 0, nil)
	s, _ = Apply(g, s, MoveRequest{PlayerID: "0", Move: "askForTarget"})
	_, err := Apply(g, s, MoveRequest{
		PlayerID: "0", Move: "pickTarget", ResumeTag: "damage", Args: []any{999},
	})
	if !errors.Is(err, ErrTargetNotCandidate) {
		t.Fatalf("expected ErrTargetNotCandidate, got %v", err)
	}
}

// TestValidateSelectionNumericCoercion verifies that int candidates
// match float64 selections (the JSON-decoded form) and vice-versa.
func TestValidateSelectionNumericCoercion(t *testing.T) {
	req := TargetRequest{
		Kind:       "pick",
		Candidates: []any{10, 20, 30},
		Min:        1, Max: 1,
	}
	// JSON-decoded numbers come back as float64.
	if err := ValidateSelection(req, []any{float64(20)}); err != nil {
		t.Fatalf("expected float64 selection to match int candidate, got %v", err)
	}
	// And the reverse — candidates from a JSON-decoded request matched
	// against native-int selections.
	jsonReq := TargetRequest{
		Kind:       "pick",
		Candidates: []any{float64(10), float64(20), float64(30)},
		Min:        1, Max: 1,
	}
	if err := ValidateSelection(jsonReq, []any{20}); err != nil {
		t.Fatalf("expected int selection to match float64 candidate, got %v", err)
	}
}

// TestValidateSelectionOpenEnded verifies a request with no Candidates
// only enforces cardinality (free-text / coordinate-style inputs).
func TestValidateSelectionOpenEnded(t *testing.T) {
	req := TargetRequest{Kind: "free", Min: 1, Max: 3}
	if err := ValidateSelection(req, []any{"anywhere", "or-here"}); err != nil {
		t.Fatalf("open-ended request should accept any selection, got %v", err)
	}
	if err := ValidateSelection(req, nil); !errors.Is(err, ErrTargetCardinality) {
		t.Fatalf("expected cardinality error on empty, got %v", err)
	}
}

// TestValidateSelectionMinMaxRange verifies a Min<Max request accepts
// any count in the range.
func TestValidateSelectionMinMaxRange(t *testing.T) {
	req := TargetRequest{
		Kind:       "draft",
		Candidates: []any{"a", "b", "c", "d"},
		Min:        2, Max: 3,
	}
	for _, sel := range [][]any{
		{"a", "b"}, {"a", "b", "c"}, {"b", "d"},
	} {
		if err := ValidateSelection(req, sel); err != nil {
			t.Fatalf("expected %v ok, got %v", sel, err)
		}
	}
	for _, sel := range [][]any{
		{"a"}, {"a", "b", "c", "d"},
	} {
		if !errors.Is(ValidateSelection(req, sel), ErrTargetCardinality) {
			t.Fatalf("expected cardinality error for %v", sel)
		}
	}
}

// TestPlainBlockStillWorks confirms backward compatibility: a Block
// call without RequestTarget produces a BlockSpec with nil Target,
// and the resume move sees that nil correctly.
func TestPlainBlockStillWorks(t *testing.T) {
	g := targetGame()
	s := NewMatch(g, 0, nil)
	s, err := Apply(g, s, MoveRequest{PlayerID: "0", Move: "plainBlock"})
	if err != nil {
		t.Fatalf("plainBlock: %v", err)
	}
	if len(s.Blocks) != 1 || s.Blocks[0].Target != nil {
		t.Fatalf("expected one plain block, got %+v", s.Blocks)
	}
	s, err = Apply(g, s, MoveRequest{
		PlayerID: "0", Move: "confirm", ResumeTag: "confirm",
	})
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if len(s.Blocks) != 0 {
		t.Fatalf("expected block consumed")
	}
}

// TestResumingBlockNilForExternalMove verifies a fresh external move
// (not a resume) starts with mc.ResumingBlock == nil.
func TestResumingBlockNilForExternalMove(t *testing.T) {
	sawNil := false
	g := &Game{
		Name: "no-resume", MinPlayers: 1, MaxPlayers: 1,
		Setup: func(_ Ctx, _ any) G { return &targetState{} },
		Moves: map[string]any{
			"x": MoveFn(func(mc *MoveContext, _ ...any) (G, error) {
				sawNil = mc.ResumingBlock == nil
				return mc.G, nil
			}),
		},
	}
	s := NewMatch(g, 0, nil)
	if _, err := Apply(g, s, MoveRequest{PlayerID: "0", Move: "x"}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !sawNil {
		t.Fatalf("expected nil ResumingBlock on non-resume move")
	}
}

// TestBlockSpecJSONRoundTrip verifies a State with a typed-Target
// block round-trips through JSON cleanly (the transport snapshots
// State this way).
func TestBlockSpecJSONRoundTrip(t *testing.T) {
	g := targetGame()
	s := NewMatch(g, 0, nil)
	s, _ = Apply(g, s, MoveRequest{PlayerID: "0", Move: "askForTarget"})

	raw, err := json.Marshal(s.Blocks)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded []BlockSpec
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(decoded) != 1 || decoded[0].Target == nil {
		t.Fatalf("Target dropped during round-trip: %+v", decoded)
	}
	got := decoded[0].Target
	if got.Kind != "damage" || got.Min != 1 || got.Max != 1 {
		t.Fatalf("round-tripped Target shape changed: %+v", got)
	}
	if len(got.Candidates) != 3 {
		t.Fatalf("candidates dropped: %+v", got.Candidates)
	}
	// And ValidateSelection still works on the decoded form, including
	// numeric coercion (JSON candidates come back as float64, selection
	// is a native int).
	if err := ValidateSelection(*got, []any{20}); err != nil {
		t.Fatalf("ValidateSelection on decoded request failed: %v", err)
	}
}
