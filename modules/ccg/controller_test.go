package ccg

import (
	"encoding/json"
	"testing"
)

func TestEffectiveControllerDefaultsToOwner(t *testing.T) {
	e := Entity{Owner: "0"}
	if got := e.EffectiveController(); got != "0" {
		t.Fatalf("EffectiveController = %q, want owner %q", got, "0")
	}
	e.Controller = "1"
	if got := e.EffectiveController(); got != "1" {
		t.Fatalf("EffectiveController = %q, want controller %q", got, "1")
	}
}

func TestControlledUsesEffectiveController(t *testing.T) {
	s := NewState()
	stolen := s.NewEntity("creature", "0", nil)
	s.SetController(stolen, "1")
	mine := s.NewEntity("creature", "1", nil)

	got := Query(s).Controlled("1").Find()
	if len(got) != 2 {
		t.Fatalf("Controlled(1) = %v, want both entities", got)
	}
	if ids := Query(s).Controlled("0").Find(); len(ids) != 0 {
		t.Fatalf("Controlled(0) = %v, want none (entity is controlled by 1)", ids)
	}
	if ids := Query(s).NotControlled("1").Find(); len(ids) != 0 {
		t.Fatalf("NotControlled(1) = %v, want none", ids)
	}
	_ = mine
}

func TestOwnedByFiltersByOwnerRegardlessOfController(t *testing.T) {
	s := NewState()
	stolen := s.NewEntity("creature", "0", nil)
	s.SetController(stolen, "1")

	ids := Query(s).OwnedBy("0").Find()
	if len(ids) != 1 || ids[0] != stolen {
		t.Fatalf("OwnedBy(0) = %v, want [%d]", ids, stolen)
	}
	if ids := Query(s).OwnedBy("1").Find(); len(ids) != 0 {
		t.Fatalf("OwnedBy(1) = %v, want none", ids)
	}
}

func TestSetControllerPersistsAndClears(t *testing.T) {
	s := NewState()
	id := s.NewEntity("creature", "0", nil)
	s.SetController(id, "1")
	e, _ := s.Get(id)
	if e.Controller != "1" {
		t.Fatalf("Controller = %q, want %q", e.Controller, "1")
	}
	s.SetController(id, "")
	e, _ = s.Get(id)
	if e.EffectiveController() != "0" {
		t.Fatalf("after clearing, EffectiveController = %q, want owner %q", e.EffectiveController(), "0")
	}
	// Unknown entity is a no-op, not a panic.
	s.SetController(EntityID(999), "1")
}

// Compat guard: an entity that never sets the new fields must marshal
// to the exact pre-change wire bytes.
func TestEntityZeroValueWireShapeUnchanged(t *testing.T) {
	raw, err := json.Marshal(Entity{ID: 1, Type: "creature", Owner: "0"})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"id":1,"type":"creature","owner":"0"}`
	if string(raw) != want {
		t.Fatalf("wire shape changed:\n got %s\nwant %s", raw, want)
	}
}

func TestModifierZeroValueWireShapeUnchanged(t *testing.T) {
	raw, err := json.Marshal(Modifier{ID: 1, Target: 2, Attribute: "power", Op: OpAdd, Value: 1, Insertion: 0})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"id":1,"target":2,"attribute":"power","op":"add","value":1,"insertion":0}`
	if string(raw) != want {
		t.Fatalf("wire shape changed:\n got %s\nwant %s", raw, want)
	}
}

func TestRedactForViewerCarriesController(t *testing.T) {
	s := NewState()
	id := s.NewEntity("creature", "0", nil)
	s.SetController(id, "1")

	view := s.RedactForViewer("2")
	e, ok := view.Get(id)
	if !ok {
		t.Fatal("entity missing from redacted view")
	}
	if e.EffectiveController() != "1" {
		t.Fatalf("redacted view lost Controller: EffectiveController = %q, want %q", e.EffectiveController(), "1")
	}
}
