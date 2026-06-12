package ccg

import (
	"errors"
	"testing"
)

func TestCloneCopiesIdentityNotPlacement(t *testing.T) {
	s := NewState()
	s.NewZone("battlefield", true)
	cat := NewCatalog()
	if err := cat.Register(CardDef{ID: "bear", Type: "creature", BaseAttrs: map[string]any{"power": 2}}); err != nil {
		t.Fatal(err)
	}
	orig, err := s.Instantiate(cat, "bear", "0")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Add("battlefield", orig); err != nil {
		t.Fatal(err)
	}
	s.SetController(orig, "1")

	cp, err := s.Clone(orig, "1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if cp == orig {
		t.Fatal("clone must mint a new EntityID")
	}
	e, ok := s.Get(cp)
	if !ok {
		t.Fatal("clone missing from state")
	}
	if e.Type != "creature" || e.DefID != "bear" {
		t.Fatalf("clone identity = (%q, %q), want (creature, bear)", e.Type, e.DefID)
	}
	if e.AttrInt("power", 0) != 2 {
		t.Fatalf("clone power = %d, want 2", e.AttrInt("power", 0))
	}
	if e.Owner != "1" {
		t.Fatalf("clone owner = %q, want %q", e.Owner, "1")
	}
	if e.Controller != "" {
		t.Fatalf("clone controller = %q, want empty (defaults to owner)", e.Controller)
	}
	if e.Zone != "" {
		t.Fatalf("clone zone = %q, want unplaced", e.Zone)
	}
}

func TestCloneDeepCopiesAttrs(t *testing.T) {
	s := NewState()
	orig := s.NewEntity("creature", "0", map[string]any{
		"tags": map[string]any{"tribe": "bear"},
	})
	cp, err := s.Clone(orig, "0", nil)
	if err != nil {
		t.Fatal(err)
	}
	ce, _ := s.Get(cp)
	ce.Attrs["tags"].(map[string]any)["tribe"] = "wolf"
	oe, _ := s.Get(orig)
	if got := oe.Attrs["tags"].(map[string]any)["tribe"]; got != "bear" {
		t.Fatalf("mutating clone's nested attrs leaked into original: tribe = %v", got)
	}
}

func TestCloneExcludesCounters(t *testing.T) {
	s := NewState()
	orig := s.NewEntity("creature", "0", map[string]any{"power": 2})
	s.AddCounter(orig, "+1/+1", 3)
	cp, err := s.Clone(orig, "0", nil)
	if err != nil {
		t.Fatal(err)
	}
	if n := s.Counters(cp, "+1/+1"); n != 0 {
		t.Fatalf("clone inherited %d counters, want 0 (counters are not template identity)", n)
	}
	if n := s.Counters(orig, "+1/+1"); n != 3 {
		t.Fatalf("original counters disturbed: %d, want 3", n)
	}
}

func TestCloneAppliesOverrides(t *testing.T) {
	s := NewState()
	orig := s.NewEntity("creature", "0", map[string]any{"power": 2, "name": "bear"})
	cp, err := s.Clone(orig, "0", map[string]any{"power": 5, "token": true})
	if err != nil {
		t.Fatal(err)
	}
	e, _ := s.Get(cp)
	if e.AttrInt("power", 0) != 5 {
		t.Fatalf("override not applied: power = %d, want 5", e.AttrInt("power", 0))
	}
	if e.AttrStr("name", "") != "bear" {
		t.Fatalf("non-overridden attr lost: name = %q", e.AttrStr("name", ""))
	}
	if !e.AttrBool("token", false) {
		t.Fatal("new override key not applied")
	}
}

func TestCloneUnknownEntity(t *testing.T) {
	s := NewState()
	if _, err := s.Clone(EntityID(42), "0", nil); !errors.Is(err, ErrUnknownEntity) {
		t.Fatalf("Clone(unknown) err = %v, want ErrUnknownEntity", err)
	}
}

func TestCloneDoesNotCopyModifiers(t *testing.T) {
	s := NewState()
	orig := s.NewEntity("creature", "0", map[string]any{"power": 2})
	s.AddModifier(Modifier{Target: orig, Attribute: "power", Op: OpAdd, Value: 2})
	cp, err := s.Clone(orig, "0", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.EffectiveAttr(cp, "power", 0); got != 2 {
		t.Fatalf("clone effective power = %v, want base 2 (modifiers must not copy)", got)
	}
}
