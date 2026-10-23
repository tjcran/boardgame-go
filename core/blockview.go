package core

// HideBlockPayload is a ready-made Game.BlockView helper for the common
// case: a pending block's Data/Target/Assignment payload should be
// visible only to the seat it's addressed to.
//
//   - When block.PlayerID == viewerID (the addressed seat), the block is
//     returned unchanged.
//   - Otherwise (other seats, and spectators — viewerID == "") the
//     payload is stripped: Data, Target and Assignment are all cleared.
//     The ownership shell (Tag / PlayerID / Move / Order) survives, so
//     clients still know a block exists, who it's waiting on, and which
//     move answers it.
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
	block.Assignment = nil
	return block
}
