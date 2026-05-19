package shop

import "github.com/tjcran/boardgame-go/modules/ccg"

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
