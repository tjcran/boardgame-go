package mcp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// SkillsDirUserGames stores designed-game specs as on-disk Claude skills,
// one directory per game. Intended for stdio (single-user) deployments
// where users want their designed games to live as human-readable,
// hand-editable files instead of in a database.
//
// Layout (under the configured root):
//
//	<root>/<game-name>/SKILL.md   — YAML frontmatter + markdown llm_guide
//	<root>/<game-name>/spec.star  — Starlark spec source
//
// Both files must be present for the directory to be recognized as a
// designed game; other subdirectories in the root (user-authored skills,
// junk) are ignored.
//
// This implementation is single-user. Owner is stored in SKILL.md
// frontmatter; cross-user isolation is by filter-on-read rather than
// by path. Multi-user stdio is unsupported by design — for hosted /
// multi-tenant deployments use PostgresUserGames instead.
type SkillsDirUserGames struct {
	mu   sync.Mutex
	root string
}

// OpenSkillsDirUserGames ensures root exists and returns a store rooted there.
func OpenSkillsDirUserGames(root string) (*SkillsDirUserGames, error) {
	if root == "" {
		return nil, errors.New("skills dir: empty path")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("skills dir: mkdir %s: %w", root, err)
	}
	return &SkillsDirUserGames{root: root}, nil
}

// Root returns the on-disk path the store reads from / writes to.
func (s *SkillsDirUserGames) Root() string { return s.root }

func (s *SkillsDirUserGames) gameDir(name string) string {
	return filepath.Join(s.root, name)
}

// Put writes the game to <root>/<name>/{SKILL.md,spec.star}. Replaces an
// existing entry by overwriting both files; created_at is preserved
// (read-modify-write) if the SKILL.md already exists.
func (s *SkillsDirUserGames) Put(_ context.Context, ug UserGame) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	dir := s.gameDir(ug.Name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("skills dir: mkdir %s: %w", dir, err)
	}

	created := ug.CreatedAt
	if created.IsZero() {
		// Preserve an existing created_at on re-Put.
		if prev, err := readSkillFile(filepath.Join(dir, "SKILL.md")); err == nil && !prev.CreatedAt.IsZero() {
			created = prev.CreatedAt
		} else {
			created = time.Now().UTC()
		}
	}

	skillMD := buildSkillMarkdown(ug.UserID, ug.Name, created, ug.LLMGuide)
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(skillMD), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "spec.star"), []byte(ug.Source), 0o644)
}

func (s *SkillsDirUserGames) Get(_ context.Context, userID, name string) (*UserGame, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ug, err := s.readGame(name)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if ug.UserID != userID {
		return nil, nil
	}
	return ug, nil
}

func (s *SkillsDirUserGames) List(_ context.Context, userID string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	games, err := s.scanGames()
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, g := range games {
		if g.UserID == userID {
			out = append(out, g.Name)
		}
	}
	sort.Strings(out)
	return out, nil
}

func (s *SkillsDirUserGames) ListAll(_ context.Context) ([]UserGame, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.scanGames()
}

func (s *SkillsDirUserGames) Delete(_ context.Context, userID, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ug, err := s.readGame(name)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if ug.UserID != userID {
		// Owner mismatch — refuse silently; the caller saw it as "not found".
		return nil
	}
	return os.RemoveAll(s.gameDir(name))
}

// readGame reads <root>/<name>/{SKILL.md,spec.star}. Returns os.ErrNotExist
// if either file is missing.
func (s *SkillsDirUserGames) readGame(name string) (*UserGame, error) {
	dir := s.gameDir(name)
	skill, err := readSkillFile(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		return nil, err
	}
	src, err := os.ReadFile(filepath.Join(dir, "spec.star"))
	if err != nil {
		return nil, err
	}
	return &UserGame{
		UserID:    skill.Owner,
		Name:      skill.Name,
		Source:    string(src),
		LLMGuide:  skill.Body,
		CreatedAt: skill.CreatedAt,
	}, nil
}

// scanGames walks the root one level deep and returns every subdirectory
// that contains both SKILL.md and spec.star with a parseable frontmatter.
// Other entries are silently ignored so the root can coexist with
// non-game skills the user (or other tools) place there.
func (s *SkillsDirUserGames) scanGames() ([]UserGame, error) {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := []UserGame{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		ug, err := s.readGame(e.Name())
		if err != nil {
			// Not a game directory — skip without surfacing.
			continue
		}
		// Name in frontmatter must match directory; defensive guard.
		if ug.Name != e.Name() {
			continue
		}
		out = append(out, *ug)
	}
	return out, nil
}

// skillFile is the parsed view of a SKILL.md.
type skillFile struct {
	Name      string
	Owner     string
	CreatedAt time.Time
	Body      string // markdown body after the frontmatter
}

// readSkillFile reads a SKILL.md and parses its YAML-ish frontmatter and
// body. The parser supports `key: value` lines only — no nested maps,
// no quoted strings — which is sufficient for our four fixed fields.
func readSkillFile(path string) (*skillFile, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	text := string(raw)
	if !strings.HasPrefix(text, "---\n") {
		return nil, fmt.Errorf("%s: missing frontmatter", path)
	}
	rest := text[len("---\n"):]
	end := strings.Index(rest, "\n---\n")
	if end < 0 {
		return nil, fmt.Errorf("%s: unterminated frontmatter", path)
	}
	front := rest[:end]
	body := strings.TrimLeft(rest[end+len("\n---\n"):], "\n")

	out := &skillFile{Body: body}
	for _, line := range strings.Split(front, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		switch k {
		case "name":
			out.Name = v
		case "owner":
			out.Owner = v
		case "created_at":
			if t, err := time.Parse(time.RFC3339, v); err == nil {
				out.CreatedAt = t
			}
		}
	}
	return out, nil
}

// buildSkillMarkdown renders a SKILL.md with the standard frontmatter
// shape this store reads. The body is the llm_guide (may be empty).
func buildSkillMarkdown(owner, name string, createdAt time.Time, llmGuide string) string {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("name: " + name + "\n")
	b.WriteString("owner: " + owner + "\n")
	b.WriteString("created_at: " + createdAt.UTC().Format(time.RFC3339) + "\n")
	b.WriteString("---\n\n")
	if llmGuide == "" {
		// Placeholder so the file is legible as a skill even without a guide.
		b.WriteString("(No strategy notes were authored for this game.)\n")
	} else {
		b.WriteString(llmGuide)
		if !strings.HasSuffix(llmGuide, "\n") {
			b.WriteString("\n")
		}
	}
	return b.String()
}
