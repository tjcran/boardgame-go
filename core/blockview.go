package core

// HideBlockPayload is a ready-made Game.BlockView helper for the common
// case: a pending block's Data/Target payload should be visible only to
// the seat it's addressed to.
//
//   - When block.PlayerID == viewerID (the addressed seat), the block is
//     returned unchanged.
//   - Otherwise (other seats, and spectators — viewerID == "") the
//     payload is stripped: Data and Target are both cleared. The
//     ownership shell (Tag / PlayerID / Order) survives, so clients still
//     know a block exists and who it's waiting on.
//
// Usage:
//
//	game := &core.Game{
//	    BlockView: core.HideBlockPayload,
//	    ...
//	}
//
// Games whose blocks carry non-hidden payloads (nothing that reveals
// hidden information — e.g. a public "choose a direction" prompt) don't
// need this; leaving BlockView nil is correct for them.
func HideBlockPayload(block BlockSpec, viewerID string) BlockSpec {
	if block.PlayerID == viewerID {
		return block
	}
	block.Data = nil
	block.Target = nil
	return block
}
