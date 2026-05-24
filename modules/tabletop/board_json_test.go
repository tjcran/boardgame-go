package tabletop

import (
	"testing"
)

func TestBoardJSON_RoundTrip(t *testing.T) {
	for _, b := range []Board{NewSquareBoard(10, 8), NewHexBoard(5, 6)} {
		raw, err := MarshalBoard(b)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		got, err := UnmarshalBoard(raw)
		if err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if got.Distance(Pos{0, 0}, Pos{1, 1}) != b.Distance(Pos{0, 0}, Pos{1, 1}) {
			t.Fatalf("board geometry not preserved across JSON (W dimension)")
		}
		if got.Distance(Pos{0, 0}, Pos{0, 2}) != b.Distance(Pos{0, 0}, Pos{0, 2}) {
			t.Fatalf("board geometry not preserved across JSON (H dimension)")
		}
		if got.InBounds(Pos{0, 0}) != b.InBounds(Pos{0, 0}) {
			t.Fatalf("in-bounds origin cell not preserved across JSON")
		}
		if got.InBounds(Pos{9999, 9999}) != b.InBounds(Pos{9999, 9999}) {
			t.Fatalf("out-of-bounds cell check not preserved across JSON")
		}
	}
}
