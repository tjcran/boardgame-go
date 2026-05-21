package starlarkgame

import (
	"encoding/json"
	"testing"
)

func TestStarlarkG_MarshalsAsBareDataWhenNoModules(t *testing.T) {
	g := &StarlarkG{Data: map[string]any{"score": int64(3)}}
	b, err := json.Marshal(g)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(b) != `{"score":3}` {
		t.Fatalf("got %s, want {\"score\":3}", b)
	}
}

func TestStarlarkG_RoundTripWithModulesKey(t *testing.T) {
	g := &StarlarkG{
		Data:    map[string]any{"score": int64(3)},
		Modules: map[string]any{"ccg": map[string]any{"zones": map[string]any{}}},
	}
	b, err := json.Marshal(g)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back StarlarkG
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.Data["score"] != float64(3) {
		t.Fatalf("data lost: %#v", back.Data)
	}
	if _, ok := back.Modules["ccg"]; !ok {
		t.Fatalf("modules key lost: %#v", back.Modules)
	}
}
