package modulebridge

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/tjcran/boardgame-go/modules/ccg"
)

// ErrBadHandle is returned when a token does not match its expected
// shape. Surfaced verbatim to Starlark (move rejected) or MCP callers.
var ErrBadHandle = errors.New("modulebridge: bad handle token")

// EntityToken renders a ccg.EntityID as the stable token "ent:<n>".
func EntityToken(id ccg.EntityID) string {
	return "ent:" + strconv.FormatUint(uint64(id), 10)
}

// ParseEntityToken is the inverse of EntityToken.
func ParseEntityToken(tok string) (ccg.EntityID, error) {
	rest, ok := strings.CutPrefix(tok, "ent:")
	if !ok || rest == "" {
		return 0, fmt.Errorf("%w: %q (want ent:<n>)", ErrBadHandle, tok)
	}
	n, err := strconv.ParseUint(rest, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: %q", ErrBadHandle, tok)
	}
	return ccg.EntityID(n), nil
}
