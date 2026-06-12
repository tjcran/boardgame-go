package ccg

import (
	"encoding/json"
	"testing"
)

func TestModifierExpiresOutsideWhileInZones(t *testing.T) {
	s := NewState()
	s.NewZone("battlefield", true)
	s.NewZone("graveyard", true)
	aura := s.NewEntity("aura", "0", nil)
	creature := s.NewEntity("creature", "0", map[string]any{"power": 2})
	if err := s.Add("battlefield", aura); err != nil {
		t.Fatal(err)
	}
	if err := s.Add("battlefield", creature); err != nil {
		t.Fatal(err)
	}
	s.AddModifier(Modifier{
		Source: aura, Target: creature, Attribute: "power", Op: OpAdd, Value: 2,
		ExpiresWith: aura, WhileIn: []ZoneName{"battlefield"},
	})

	if got := s.EffectiveAttr(creature, "power", 0); got != 4 {
		t.Fatalf("with aura on battlefield: power = %v, want 4", got)
	}
	if err := s.MoveTo(aura, "graveyard"); err != nil {
		t.Fatal(err)
	}
	if got := s.EffectiveAttr(creature, "power", 0); got != 2 {
		t.Fatalf("after aura left battlefield: power = %v, want 2", got)
	}
	// Reads filter but never delete — persisted bytes must not depend
	// on who looked at the state. Deletion is explicit.
	if n := len(s.Modifiers); n != 1 {
		t.Fatalf("reads must not delete expired modifiers, have %d", n)
	}
	if removed := s.SweepModifiers(); len(removed) != 1 {
		t.Fatalf("SweepModifiers = %v, want one removal", removed)
	}
	if n := len(s.Modifiers); n != 0 {
		t.Fatalf("expired modifier should be gone after sweep, %d remain", n)
	}
}

func TestModifierExpiresWithThirdEntityDestroyed(t *testing.T) {
	s := NewState()
	host := s.NewEntity("enchantment", "0", nil)
	creature := s.NewEntity("creature", "1", map[string]any{"power": 1})
	s.AddModifier(Modifier{
		Target: creature, Attribute: "power", Op: OpAdd, Value: 3,
		ExpiresWith: host,
	})

	if got := s.EffectiveAttr(creature, "power", 0); got != 4 {
		t.Fatalf("before destroy: power = %v, want 4", got)
	}
	s.Destroy(host)
	if got := s.EffectiveAttr(creature, "power", 0); got != 1 {
		t.Fatalf("after ExpiresWith entity destroyed: power = %v, want 1", got)
	}
	// Destroy is a mutation point: it removes modifiers lifetime-linked
	// to the destroyed entity, same as Source/Target-linked ones.
	if n := len(s.Modifiers); n != 0 {
		t.Fatalf("Destroy should remove ExpiresWith-linked modifiers, %d remain", n)
	}
}

// Zero value keeps today's behavior: a modifier without ExpiresWith
// survives its Source moving zones.
func TestModifierWithoutExpiresWithSurvivesSourceMove(t *testing.T) {
	s := NewState()
	s.NewZone("battlefield", true)
	s.NewZone("graveyard", true)
	src := s.NewEntity("aura", "0", nil)
	creature := s.NewEntity("creature", "0", map[string]any{"power": 2})
	if err := s.Add("battlefield", src); err != nil {
		t.Fatal(err)
	}
	s.AddModifier(Modifier{Source: src, Target: creature, Attribute: "power", Op: OpAdd, Value: 2})

	if err := s.MoveTo(src, "graveyard"); err != nil {
		t.Fatal(err)
	}
	if got := s.EffectiveAttr(creature, "power", 0); got != 4 {
		t.Fatalf("modifier without ExpiresWith must persist: power = %v, want 4", got)
	}
}

// WhileIn without ExpiresWith has nothing to track and is ignored.
func TestWhileInWithoutExpiresWithIsIgnored(t *testing.T) {
	s := NewState()
	creature := s.NewEntity("creature", "0", map[string]any{"power": 2})
	s.AddModifier(Modifier{
		Target: creature, Attribute: "power", Op: OpAdd, Value: 2,
		WhileIn: []ZoneName{"battlefield"},
	})
	if got := s.EffectiveAttr(creature, "power", 0); got != 4 {
		t.Fatalf("WhileIn without ExpiresWith must be ignored: power = %v, want 4", got)
	}
}

func TestSweepModifiersEager(t *testing.T) {
	s := NewState()
	s.NewZone("battlefield", true)
	host := s.NewEntity("aura", "0", nil)
	creature := s.NewEntity("creature", "0", nil)
	kept := s.AddModifier(Modifier{Target: creature, Attribute: "power", Op: OpAdd, Value: 1})
	dead := s.AddModifier(Modifier{
		Target: creature, Attribute: "power", Op: OpAdd, Value: 1,
		ExpiresWith: host, WhileIn: []ZoneName{"battlefield"},
	})

	removed := s.SweepModifiers()
	if len(removed) != 1 || removed[0] != dead {
		t.Fatalf("SweepModifiers = %v, want [%d]", removed, dead)
	}
	if _, ok := s.Modifiers[kept]; !ok {
		t.Fatal("unexpired modifier was swept")
	}
	if _, ok := s.Modifiers[dead]; ok {
		t.Fatal("expired modifier survived sweep")
	}
}

func TestModifierLifetimeFieldsRoundTripJSON(t *testing.T) {
	s := NewState()
	s.NewZone("battlefield", true)
	host := s.NewEntity("aura", "0", nil)
	if err := s.Add("battlefield", host); err != nil {
		t.Fatal(err)
	}
	creature := s.NewEntity("creature", "0", nil)
	id := s.AddModifier(Modifier{
		Target: creature, Attribute: "power", Op: OpAdd, Value: 1,
		ExpiresWith: host, WhileIn: []ZoneName{"battlefield"},
	})

	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	loaded := NewState()
	if err := json.Unmarshal(raw, loaded); err != nil {
		t.Fatal(err)
	}
	m, ok := loaded.Modifiers[id]
	if !ok {
		t.Fatal("modifier missing after round trip")
	}
	if m.ExpiresWith != host {
		t.Fatalf("ExpiresWith = %d, want %d", m.ExpiresWith, host)
	}
	if len(m.WhileIn) != 1 || m.WhileIn[0] != "battlefield" {
		t.Fatalf("WhileIn = %v, want [battlefield]", m.WhileIn)
	}
}
