package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// FlatFile persists each match as a JSON file in a directory. Mirrors
// boardgame.io's FlatFile adapter (which wraps node-persist).
//
// Concurrency: an in-process RWMutex guards reads and writes; the mutex
// does NOT protect against external processes touching the same directory.
// For multi-process deployments use a real database adapter (TODO).
type FlatFile struct {
	dir string

	mu sync.RWMutex
}

// NewFlatFile creates the directory if it doesn't exist and returns a
// FlatFile backed by it.
func NewFlatFile(dir string) (*FlatFile, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("flatfile: mkdir %s: %w", dir, err)
	}
	return &FlatFile{dir: dir}, nil
}

// Create writes a new match file. Returns an error if the file already
// exists (parity with Memory.Create behaviour).
func (f *FlatFile) Create(m *Match) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	path := f.matchPath(m.ID)
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("flatfile: match %s already exists", m.ID)
	}
	return f.write(path, m)
}

// Get reads a match from disk. Returns ErrNotFound if the file doesn't
// exist.
func (f *FlatFile) Get(id string) (*Match, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.read(id)
}

// Update overwrites an existing match file. Returns ErrNotFound if the
// match doesn't exist yet (use Create for new matches).
func (f *FlatFile) Update(m *Match) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	path := f.matchPath(m.ID)
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return ErrNotFound
		}
		return err
	}
	return f.write(path, m)
}

// List walks the directory and returns every match (filtered by gameName
// if non-empty). The list isn't paginated — fine for dev / small fleets,
// not OK for a real production datastore.
func (f *FlatFile) List(gameName string) ([]*Match, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	entries, err := os.ReadDir(f.dir)
	if err != nil {
		return nil, err
	}
	var out []*Match
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".json")
		m, err := f.read(id)
		if err != nil {
			continue
		}
		if gameName != "" && m.GameName != gameName {
			continue
		}
		out = append(out, m)
	}
	return out, nil
}

// Wipe deletes a match's file from disk.
func (f *FlatFile) Wipe(id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	err := os.Remove(f.matchPath(id))
	if os.IsNotExist(err) {
		return ErrNotFound
	}
	return err
}

func (f *FlatFile) matchPath(id string) string {
	return filepath.Join(f.dir, id+".json")
}

func (f *FlatFile) write(path string, m *Match) error {
	tmp, err := os.CreateTemp(f.dir, ".tmp-*.json")
	if err != nil {
		return err
	}
	enc := json.NewEncoder(tmp)
	enc.SetIndent("", "  ")
	if err := enc.Encode(m); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	// Atomic rename so partial writes never leave a half-baked file
	// readable by a concurrent Get.
	return os.Rename(tmp.Name(), path)
}

func (f *FlatFile) read(id string) (*Match, error) {
	data, err := os.ReadFile(f.matchPath(id))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var m Match
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("flatfile: decode %s: %w", id, err)
	}
	return &m, nil
}
