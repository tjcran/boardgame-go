package starlarkgame

// legalMoveName returns the move name from one entry of the spec's
// legal_moves() return value. Either "name" or "move" is accepted because:
//
//   - The design-a-game prompt told authors to use "name" (the historical
//     convention that mirrored how the spec's MOVES dict is keyed).
//   - But the MCP response shape uses "move" (matching core.EnumerateAction's
//     JSON tag), and authors who copy the response shape naturally write
//     "move" in their legal_moves dicts.
//
// Accepting both unblocks every spec without a forced re-write. "move" is
// the recommended key going forward; the prompt was updated to match.
func legalMoveName(entry map[string]any) string {
	if v, ok := entry["move"].(string); ok && v != "" {
		return v
	}
	if v, ok := entry["name"].(string); ok {
		return v
	}
	return ""
}
