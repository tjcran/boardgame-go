package ccg

import (
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"testing"
)

func TestToAssignmentPairs(t *testing.T) {
	got := ToAssignmentPairs([][2]EntityID{{3, 1}, {4, 2}})
	want := [][2]any{{EntityID(3), EntityID(1)}, {EntityID(4), EntityID(2)}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
	if got := ToAssignmentPairs(nil); got != nil {
		t.Fatalf("nil input: got %#v", got)
	}
	if got := ToAssignmentPairs([][2]EntityID{}); got != nil {
		t.Fatalf("empty input: got %#v", got)
	}
}

func TestEntityPairs_CoercesIntegralSides(t *testing.T) {
	got, err := EntityPairs([][2]any{
		{EntityID(1), int(2)},
		{int64(3), uint64(4)},
		{float64(5), float64(1 << 40)},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := [][2]EntityID{{1, 2}, {3, 4}, {5, 1 << 40}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	if got, err := EntityPairs(nil); got != nil || err != nil {
		t.Fatalf("nil input: %v, %v", got, err)
	}
}

// TestEntityPairs_JSONRoundTrip: typed pairs survive the trip out as
// ToAssignmentPairs values and back as decoded float64s.
func TestEntityPairs_JSONRoundTrip(t *testing.T) {
	in := [][2]EntityID{{7, 1}, {8, 2}}
	raw, err := json.Marshal(ToAssignmentPairs(in))
	if err != nil {
		t.Fatal(err)
	}
	var decoded [][2]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if _, ok := decoded[0][0].(float64); !ok {
		t.Fatalf("decoded side is %T, want float64", decoded[0][0])
	}
	got, err := EntityPairs(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, in) {
		t.Fatalf("got %v want %v", got, in)
	}
}

func TestEntityPairs_RejectsNonIDs(t *testing.T) {
	for name, side := range map[string]any{
		"fractional float": 1.5,
		"negative float":   float64(-1),
		"float past range": math.Ldexp(1, 64),
		"NaN":              math.NaN(),
		"infinity":         math.Inf(1),
		"negative int":     int(-1),
		"negative int64":   int64(-1),
		"string":           "1",
		"nil":              nil,
		"int32":            int32(1),
		"nested slice":     []any{1.0},
	} {
		got, err := EntityPairs([][2]any{{EntityID(1), EntityID(2)}, {EntityID(3), side}})
		if !errors.Is(err, ErrBadPair) {
			t.Errorf("%s: err = %v, want ErrBadPair", name, err)
		}
		if got != nil {
			t.Errorf("%s: pairs on error = %v, want nil", name, got)
		}
	}
}
