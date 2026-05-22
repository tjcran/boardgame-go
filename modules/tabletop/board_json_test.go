package tabletop

import (
	"encoding/json"
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
			t.Fatalf("board geometry not preserved across JSON")
		}
		_ = json.RawMessage(raw)
	}
}
