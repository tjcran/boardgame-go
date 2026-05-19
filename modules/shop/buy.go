package shop

import "github.com/tjcran/boardgame-go/modules/ccg"

// Buy moves item from Slots to destination — typically the player's
// hand, bench, or board. Returns ErrNotInSlots when item is not in
// Slots, or ccg.ErrUnknownZone / ccg.ErrZoneFull when destination is
// missing or capped.
//
// Buy does NOT charge a cost. Combine with economy.Pool.Spend in the
// move handler:
//
//	if err := gold.Spend(s, item.AttrInt("cost", 0)); err != nil {
//	    return g, err  // ErrInsufficient — Buy never runs
//	}
//	if err := market.Buy(s, item, "hand"); err != nil {
//	    // refund — pool wasn't moved by ccg yet only if Spend ran
//	    return g, err
//	}
//
// Buy clears the freeze flag on the bought item so shop-state doesn't
// leak into the destination zone.
func (sh Shop) Buy(s *ccg.State, item ccg.EntityID, destination ccg.ZoneName) error {
	if !s.Contains(sh.Slots, item) {
		return ErrNotInSlots
	}
	if _, ok := s.Zones[destination]; !ok {
		return ccg.ErrUnknownZone
	}
	if err := s.MoveTo(item, destination); err != nil {
		return err
	}
	// Clear freeze flag so the item doesn't carry shop state into
	// the hand / bench.
	_ = sh.Unfreeze(s, item)
	return nil
}
