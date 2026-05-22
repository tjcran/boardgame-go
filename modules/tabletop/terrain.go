package tabletop

import "encoding/json"

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

// terrainCellJSON is the on-wire representation for one cell's tag set.
type terrainCellJSON struct {
	Pos  Pos      `json:"pos"`
	Tags []string `json:"tags"`
}

// MarshalJSON encodes TerrainMap as a JSON array of {pos, tags} objects.
// Using a slice avoids the map-with-struct-key limitation in encoding/json.
func (t *TerrainMap) MarshalJSON() ([]byte, error) {
	cells := make([]terrainCellJSON, 0, len(t.Cells))
	for p, tagSet := range t.Cells {
		tags := make([]string, 0, len(tagSet))
		for tag := range tagSet {
			tags = append(tags, tag)
		}
		cells = append(cells, terrainCellJSON{Pos: p, Tags: tags})
	}
	return json.Marshal(cells)
}

// UnmarshalJSON reconstructs a TerrainMap from the array format written by
// MarshalJSON.
func (t *TerrainMap) UnmarshalJSON(data []byte) error {
	var cells []terrainCellJSON
	if err := json.Unmarshal(data, &cells); err != nil {
		return err
	}
	t.Cells = make(map[Pos]map[string]bool, len(cells))
	for _, c := range cells {
		tagSet := make(map[string]bool, len(c.Tags))
		for _, tag := range c.Tags {
			tagSet[tag] = true
		}
		t.Cells[c.Pos] = tagSet
	}
	return nil
}
