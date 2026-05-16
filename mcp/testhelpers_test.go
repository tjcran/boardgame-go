package mcp

import "context"

// contextWithUser is a test helper that stamps a userID onto ctx the same
// way the production HTTP transport does. Lives here (not in production code)
// because it is only ever called from _test.go files.
func contextWithUser(ctx context.Context, userID string) context.Context {
	return WithUserID(ctx, userID)
}
