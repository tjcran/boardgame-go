package typed_test

import (
	"errors"
	"testing"

	"github.com/tjcran/boardgame-go/modules/ccg"
	"github.com/tjcran/boardgame-go/modules/ccg/typed"
)

type goblinAttrs struct {
	Power     int `json:"power"`
	Toughness int `json:"toughness"`
}

func TestRegisterAndGet(t *testing.T) {
	c := typed.NewCatalog[goblinAttrs]()
	def := typed.CardDef[goblinAttrs]{
		ID:    "goblin",
		Type:  "creature",
		Attrs: goblinAttrs{Power: 2, Toughness: 1},
	}
	if err := c.Register(def); err != nil {
		t.Fatalf("register: %v", err)
	}
	got, ok := c.Get("goblin")
	if !ok {
		t.Fatalf("Get(goblin) after register: not found")
	}
	if got.Type != "creature" || got.Attrs.Power != 2 || got.Attrs.Toughness != 1 {
		t.Fatalf("typed def mismatch: %+v", got)
	}
	if c.Len() != 1 {
		t.Fatalf("Len: want 1, got %d", c.Len())
	}
}

func TestRegisterFlattensToUntypedCatalog(t *testing.T) {
	c := typed.NewCatalog[goblinAttrs]()
	_ = c.Register(typed.CardDef[goblinAttrs]{
		ID:    "goblin",
		Type:  "creature",
		Attrs: goblinAttrs{Power: 2, Toughness: 1},
	})

	// The Untyped() catalog should now have a CardDef whose BaseAttrs
	// contains the flattened fields keyed by json tag.
	u, ok := c.Untyped().Get("goblin")
	if !ok {
		t.Fatalf("untyped catalog missing the flattened def")
	}
	// Numbers come back as float64 after the JSON round-trip.
	if u.BaseAttrs["power"] != float64(2) || u.BaseAttrs["toughness"] != float64(1) {
		t.Fatalf("flattened attrs: %+v", u.BaseAttrs)
	}
}

func TestRegisterRejectsPrimitiveAttrs(t *testing.T) {
	c := typed.NewCatalog[int]()
	err := c.Register(typed.CardDef[int]{ID: "x", Attrs: 5})
	if !errors.Is(err, typed.ErrNonObjectAttrs) {
		t.Fatalf("primitive Attrs: want ErrNonObjectAttrs, got %v", err)
	}
	// Catalog must remain untouched after the failed Register.
	if c.Len() != 0 {
		t.Fatalf("failed register leaked into catalog: %d", c.Len())
	}
	if _, ok := c.Untyped().Get("x"); ok {
		t.Fatalf("failed register leaked into untyped catalog")
	}
}

func TestRegisterDuplicate(t *testing.T) {
	c := typed.NewCatalog[goblinAttrs]()
	_ = c.Register(typed.CardDef[goblinAttrs]{ID: "x"})
	err := c.Register(typed.CardDef[goblinAttrs]{ID: "x"})
	if !errors.Is(err, ccg.ErrDuplicateDef) {
		t.Fatalf("duplicate register: want ErrDuplicateDef, got %v", err)
	}
}

func TestRegisterAllowsEmptyStructAttrs(t *testing.T) {
	type vanilla struct{}
	c := typed.NewCatalog[vanilla]()
	if err := c.Register(typed.CardDef[vanilla]{ID: "x", Type: "creature"}); err != nil {
		t.Fatalf("empty-struct Attrs should register: %v", err)
	}
}

func TestRegisterWithMapAttrs(t *testing.T) {
	// map[string]X is also an object-shaped JSON value; accept it.
	c := typed.NewCatalog[map[string]int]()
	if err := c.Register(typed.CardDef[map[string]int]{
		ID: "x", Attrs: map[string]int{"power": 3},
	}); err != nil {
		t.Fatalf("map[string]int Attrs should register: %v", err)
	}
	got, _ := c.Get("x")
	if got.Attrs["power"] != 3 {
		t.Fatalf("typed map Attrs: %+v", got.Attrs)
	}
}

func TestGetByEntityIDRoundTrip(t *testing.T) {
	c := typed.NewCatalog[goblinAttrs]()
	_ = c.Register(typed.CardDef[goblinAttrs]{
		ID:    "goblin",
		Type:  "creature",
		Attrs: goblinAttrs{Power: 2, Toughness: 1},
	})

	s := ccg.NewState()
	id, err := s.Instantiate(c.Untyped(), "goblin", "0")
	if err != nil {
		t.Fatalf("Instantiate via Untyped: %v", err)
	}

	// Typed lookup from runtime entity.
	def, ok := typed.Get(s, c, id)
	if !ok {
		t.Fatalf("typed.Get: not found")
	}
	if def.ID != "goblin" || def.Attrs.Power != 2 || def.Attrs.Toughness != 1 {
		t.Fatalf("typed.Get returned wrong def: %+v", def)
	}
	// Sanity: the runtime entity also carries the flattened attrs via
	// the untyped path.
	e, _ := s.Get(id)
	if e.AttrInt("power", 0) != 2 {
		t.Fatalf("untyped runtime read: %+v", e.Attrs)
	}
}

func TestGetByEntityIDMissingPaths(t *testing.T) {
	c := typed.NewCatalog[goblinAttrs]()
	s := ccg.NewState()

	// Unknown entity ID.
	if _, ok := typed.Get(s, c, ccg.EntityID(9999)); ok {
		t.Fatalf("unknown entity should return false")
	}
	// Entity with empty DefID (minted via NewEntity, not Instantiate).
	id := s.NewEntity("creature", "0", nil)
	if _, ok := typed.Get(s, c, id); ok {
		t.Fatalf("entity without DefID should return false")
	}
}

func TestGetByEntityIDDefIDNotInCatalog(t *testing.T) {
	// Game registers a def in *one* catalog, instantiates from it,
	// then queries a *different* typed catalog with no record of
	// that DefID — typed.Get should return false rather than panic.
	c1 := typed.NewCatalog[goblinAttrs]()
	_ = c1.Register(typed.CardDef[goblinAttrs]{ID: "goblin", Type: "creature"})
	s := ccg.NewState()
	id, _ := s.Instantiate(c1.Untyped(), "goblin", "0")

	c2 := typed.NewCatalog[goblinAttrs]()
	if _, ok := typed.Get(s, c2, id); ok {
		t.Fatalf("expected false when entity's DefID isn't in this catalog")
	}
}
