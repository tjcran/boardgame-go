package shop

import (
	"github.com/tjcran/boardgame-go/core"
	"github.com/tjcran/boardgame-go/modules/ccg"
)

// Clear removes every un-frozen entity from Slots. If dest is empty,
// removed entities are destroyed (gone from ccg.State entirely); if
// dest names a known zone, they move there instead. Frozen entities
// are skipped.
//
// dest == "" is the typical TFT-shop / Slay-the-Spire-shop semantic;
// dest = "some-discard" matches Hearthstone-BG-style "returned to the
// shared pool."
func (sh Shop) Clear(s *ccg.State, dest ccg.ZoneName) error {
	z, ok := s.Zones[sh.Slots]
	if !ok {
		return ccg.ErrUnknownZone
	}
	// Snapshot membership first — Members shifts as we remove/move.
	victims := make([]ccg.EntityID, 0, len(z.Members))
	for _, id := range z.Members {
		if !sh.IsFrozen(s, id) {
			victims = append(victims, id)
		}
	}
	if dest == "" {
		for _, id := range victims {
			s.Destroy(id)
		}
		return nil
	}
	if _, ok := s.Zones[dest]; !ok {
		return ccg.ErrUnknownZone
	}
	for _, id := range victims {
		if err := s.MoveTo(id, dest); err != nil {
			return err
		}
	}
	return nil
}

// Fill draws from Stock to bring Slots up to Size, stopping early if
// Stock runs dry. Frozen items in Slots count toward the total — a
// shop with 2 frozen items and Size=5 will draw at most 3 from Stock.
//
// Fill does NOT shuffle Stock; pass a pre-shuffled deck-style zone, or
// use ccg.State.Shuffle on Stock between rolls.
func (sh Shop) Fill(s *ccg.State, r *core.Random) error {
	slots, ok := s.Zones[sh.Slots]
	if !ok {
		return ccg.ErrUnknownZone
	}
	stock, ok := s.Zones[sh.Stock]
	if !ok {
		return ccg.ErrUnknownZone
	}
	want := sh.Size - len(slots.Members)
	if want <= 0 {
		return nil
	}
	if want > len(stock.Members) {
		want = len(stock.Members)
	}
	if want == 0 {
		return nil
	}
	drawn, err := s.Draw(sh.Stock, want)
	if err != nil {
		return err
	}
	for _, id := range drawn {
		if err := s.MoveTo(id, sh.Slots); err != nil {
			return err
		}
	}
	return nil
}

// Roll is sugar for Clear(dest) followed by Fill(r). The typical
// "spend 1 gold to reroll the shop" move is exactly Roll(s, mc.Random, "").
func (sh Shop) Roll(s *ccg.State, r *core.Random, dest ccg.ZoneName) error {
	if err := sh.Clear(s, dest); err != nil {
		return err
	}
	return sh.Fill(s, r)
}
