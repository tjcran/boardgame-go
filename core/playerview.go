package core

// StripSecrets is the built-in PlayerView helper that mirrors BGIO's
// `PlayerView.STRIP_SECRETS`:
//
//   - Removes the `secret` key from G.
//   - If G has a `players` map, keeps only the entry for the requesting
//     playerID (spectators see no entries).
//
// G must be a map[string]any. For struct-shaped G, games should write a
// PlayerView function tailored to their schema — there's no generic way to
// strip fields from an arbitrary struct.
//
// Usage:
//
//	game := &core.Game{
//	    PlayerView: core.StripSecrets,
//	    ...
//	}
func StripSecrets(g G, _ Ctx, playerID string) G {
	m, ok := g.(map[string]any)
	if !ok {
		return g
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		if k == "secret" {
			continue
		}
		if k == "players" {
			players, ok := v.(map[string]any)
			if !ok {
				out[k] = v
				continue
			}
			filtered := map[string]any{}
			if playerID != "" {
				if p, ok := players[playerID]; ok {
					filtered[playerID] = p
				}
			}
			out[k] = filtered
			continue
		}
		out[k] = v
	}
	return out
}
