package tabletop

// TerrainTagBlocksLOS is the conventional tag name for "this cell
// blocks line of sight." Callers are free to use their own tags;
// Blocks is sugar for HasTag(p, TerrainTagBlocksLOS).
const TerrainTagBlocksLOS = "blocks_los"

// TerrainMap is a sparse per-cell tag store. Cells without tags don't
// occupy a map slot. Tag names are arbitrary game-defined strings
// ("cover", "blocks_los", "difficult", "objective_zone", …).
//
// TerrainMap is JSON-friendly when marshalled via the standard
// encoding/json: Pos becomes "{X,Y}" via Go's default struct
// marshalling (which works because Pos has no map keys).
type TerrainMap struct {
	Cells map[Pos]map[string]bool `json:"cells,omitempty"`
}

// NewTerrainMap returns an empty TerrainMap.
func NewTerrainMap() *TerrainMap {
	return &TerrainMap{Cells: map[Pos]map[string]bool{}}
}

// Tag adds tag to cell p. Idempotent — re-tagging is a no-op.
func (t *TerrainMap) Tag(p Pos, tag string) {
	if t.Cells == nil {
		t.Cells = map[Pos]map[string]bool{}
	}
	tags, ok := t.Cells[p]
	if !ok {
		tags = map[string]bool{}
		t.Cells[p] = tags
	}
	tags[tag] = true
}

// Untag removes tag from cell p. If the cell has no remaining tags,
// the cell entry is removed entirely so HasTag iteration stays sparse.
func (t *TerrainMap) Untag(p Pos, tag string) {
	tags, ok := t.Cells[p]
	if !ok {
		return
	}
	delete(tags, tag)
	if len(tags) == 0 {
		delete(t.Cells, p)
	}
}

// HasTag reports whether cell p carries tag.
func (t *TerrainMap) HasTag(p Pos, tag string) bool {
	if t.Cells == nil {
		return false
	}
	tags, ok := t.Cells[p]
	if !ok {
		return false
	}
	return tags[tag]
}

// Blocks is sugar for HasTag(p, TerrainTagBlocksLOS). Drop a TerrainMap
// straight into LineOfSight via:
//
//	tabletop.LineOfSight(board, from, to, terrain.Blocks)
func (t *TerrainMap) Blocks(p Pos) bool {
	return t.HasTag(p, TerrainTagBlocksLOS)
}
