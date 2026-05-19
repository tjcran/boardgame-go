package shop

import (
	"errors"

	"github.com/tjcran/boardgame-go/modules/ccg"
)

// ShopFrozenAttrKey is the reserved Entity.Attrs key for the freeze
// flag. Games must not set this directly — use Freeze / Unfreeze.
const ShopFrozenAttrKey = "__shop_frozen"

// Shop is the config handle. State lives in the two named ccg zones —
// Shop itself is a few words on the stack and can be rebuilt from
// constants in move handlers if you don't want to keep it around.
type Shop struct {
	// Slots is the visible market row (an ordered or unordered
	// ccg.Zone declared by the game).
	Slots ccg.ZoneName
	// Stock is the supply the row refills from. Often an ordered zone
	// holding a shuffled pool of available items.
	Stock ccg.ZoneName
	// Size is the target number of items in Slots after a Fill.
	// Fill will draw at most (Size - len(Slots)) items.
	Size int
}

// ErrNotInSlots is returned by Buy and Freeze when the targeted entity
// is not currently in the shop's Slots zone.
var ErrNotInSlots = errors.New("shop: entity is not in shop slots")
