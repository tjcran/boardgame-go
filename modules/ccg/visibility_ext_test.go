package ccg

import (
	"encoding/json"
	"testing"
)

func TestRevealedToGrantsFullVisibility(t *testing.T) {
	s := NewState()
	id := s.NewEntity("card", "0", map[string]any{"rank": "ace"})
	s.SetVisibility(id, OwnerOnly)
	s.RevealTo(id, "2")

	e, _ := s.Get(id)
	if !e.IsVisibleTo("2") {
		t.Fatal("RevealedTo viewer must see the entity")
	}
	if !e.IsVisibleTo("0") {
		t.Fatal("owner must still see their OwnerOnly entity")
	}
	if e.IsVisibleTo("3") {
		t.Fatal("unrelated viewer must not see an OwnerOnly entity")
	}

	view := s.RedactForViewer("2")
	ve, _ := view.Get(id)
	if ve.AttrStr("rank", "") != "ace" {
		t.Fatalf("revealed viewer got a stub: attrs = %v", ve.Attrs)
	}
	other := s.RedactForViewer("3")
	oe, _ := other.Get(id)
	if oe.Type != "" || oe.AttrStr("rank", "") != "" {
		t.Fatalf("unrelated viewer must get a stub, got %+v", oe)
	}
}

func TestRevealedToOverridesHidden(t *testing.T) {
	s := NewState()
	id := s.NewEntity("card", "0", nil)
	s.SetVisibility(id, Hidden)
	s.RevealTo(id, "1")

	e, _ := s.Get(id)
	if !e.IsVisibleTo("1") {
		t.Fatal("RevealedTo must override Hidden for the listed viewer")
	}
	if e.IsVisibleTo("0") {
		t.Fatal("Hidden entity must stay hidden from its owner unless revealed")
	}
}

func TestConcealFromRemovesViewer(t *testing.T) {
	s := NewState()
	id := s.NewEntity("card", "0", nil)
	s.SetVisibility(id, OwnerOnly)
	s.RevealTo(id, "1", "2")
	s.ConcealFrom(id, "1")

	e, _ := s.Get(id)
	if e.IsVisibleTo("1") {
		t.Fatal("concealed viewer must lose visibility")
	}
	if !e.IsVisibleTo("2") {
		t.Fatal("other revealed viewer must keep visibility")
	}
	// Idempotent / unknown-entity safe.
	s.ConcealFrom(id, "1")
	s.ConcealFrom(EntityID(99), "1")
}

func TestMaskAppearsInRedactedStub(t *testing.T) {
	s := NewState()
	id := s.NewEntity("creature", "0", map[string]any{"name": "dragon", "power": 9})
	s.SetVisibility(id, OwnerOnly)
	s.SetMask(id, map[string]any{"frame": "creature", "power_hint": 2})

	view := s.RedactForViewer("1")
	e, _ := view.Get(id)
	if e.AttrStr("frame", "") != "creature" || e.AttrInt("power_hint", 0) != 2 {
		t.Fatalf("stub must carry mask attrs, got %v", e.Attrs)
	}
	if e.AttrStr("name", "") != "" || e.AttrInt("power", 0) != 0 {
		t.Fatalf("stub leaked real attrs: %v", e.Attrs)
	}
	if e.Type != "" || e.Owner != "" {
		t.Fatalf("mask must not restore identity fields: %+v", e)
	}
	if len(e.Mask) != 0 {
		t.Fatal("stub should express the mask via Attrs, not carry Mask itself")
	}

	owner := s.RedactForViewer("0")
	oe, _ := owner.Get(id)
	if oe.AttrStr("name", "") != "dragon" {
		t.Fatal("owner must still see real attrs")
	}
	if len(oe.Mask) == 0 {
		t.Fatal("owner's full copy should carry Mask so the game can edit it")
	}
}

func TestMaskStubAttrsDoNotAliasOriginal(t *testing.T) {
	s := NewState()
	id := s.NewEntity("card", "0", nil)
	s.SetVisibility(id, Hidden)
	s.SetMask(id, map[string]any{"tags": map[string]any{"frame": "creature"}})

	view := s.RedactForViewer("1")
	e, _ := view.Get(id)
	e.Attrs["tags"].(map[string]any)["frame"] = "tampered"

	orig, _ := s.Get(id)
	if got := orig.Mask["tags"].(map[string]any)["frame"]; got != "creature" {
		t.Fatalf("stub attrs alias the original mask: %v", got)
	}
}

func TestRevealedToAndMaskRoundTripJSON(t *testing.T) {
	s := NewState()
	id := s.NewEntity("card", "0", nil)
	s.SetVisibility(id, OwnerOnly)
	s.RevealTo(id, "2")
	s.SetMask(id, map[string]any{"frame": "creature"})

	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	loaded := NewState()
	if err := json.Unmarshal(raw, loaded); err != nil {
		t.Fatal(err)
	}
	e, ok := loaded.Get(id)
	if !ok {
		t.Fatal("entity missing after round trip")
	}
	if len(e.RevealedTo) != 1 || e.RevealedTo[0] != "2" {
		t.Fatalf("RevealedTo = %v, want [2]", e.RevealedTo)
	}
	if e.Mask["frame"] != "creature" {
		t.Fatalf("Mask = %v, want frame=creature", e.Mask)
	}
}
