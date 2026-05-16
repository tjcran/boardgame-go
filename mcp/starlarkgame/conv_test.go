package starlarkgame

import (
	"reflect"
	"testing"

	"go.starlark.net/starlark"
)

func TestToGoFromStarlark_Primitives(t *testing.T) {
	cases := []struct{
		in   starlark.Value
		want any
	}{
		{starlark.None, nil},
		{starlark.Bool(true), true},
		{starlark.MakeInt(42), int64(42)},
		{starlark.Float(3.5), 3.5},
		{starlark.String("hi"), "hi"},
	}
	for _, c := range cases {
		got, err := ToGo(c.in)
		if err != nil { t.Fatalf("ToGo(%v): %v", c.in, err) }
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("ToGo(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestToGoFromStarlark_DictAndList(t *testing.T) {
	d := starlark.NewDict(0)
	_ = d.SetKey(starlark.String("name"), starlark.String("tic"))
	_ = d.SetKey(starlark.String("cells"), starlark.NewList([]starlark.Value{
		starlark.None, starlark.String("0"), starlark.None,
	}))
	got, err := ToGo(d)
	if err != nil { t.Fatalf("ToGo dict: %v", err) }
	want := map[string]any{
		"name":  "tic",
		"cells": []any{nil, "0", nil},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ToGo dict = %#v, want %#v", got, want)
	}
}

func TestToStarlarkFromGo_RoundTrip(t *testing.T) {
	in := map[string]any{
		"cells": []any{nil, "X", nil},
		"score": int64(7),
		"on":    true,
	}
	sv, err := ToStarlark(in)
	if err != nil { t.Fatalf("ToStarlark: %v", err) }
	back, err := ToGo(sv)
	if err != nil { t.Fatalf("ToGo (back): %v", err) }
	if !reflect.DeepEqual(in, back) {
		t.Errorf("round trip = %#v, want %#v", back, in)
	}
}
