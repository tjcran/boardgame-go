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

	"github.com/tjcran/boardgame-go/mcp/starlarkgame"
)

// SkillsDirUserGames stores designed-game specs as on-disk Claude skills,
// one directory per game. Intended for stdio (single-user) deployments
// where users want their designed games to live as human-readable,
// hand-editable files instead of in a database.
//
// Layout (under the configured root):
//
//	<root>/<game-name>/SKILL.md   — auto-generated rich rendering
//	                                (frontmatter + moves table + sections).
//	                                Regenerated on every Put; hand-edits
//	                                to it are lost on the next save.
//	<root>/<game-name>/spec.star  — canonical Starlark spec source.
//	<root>/<game-name>/guide.md   — canonical llm_guide content
//	                                (omitted if the designer didn't write one).
//
// SKILL.md and spec.star are required for a directory to be recognised
// as a designed game; guide.md is optional. Other subdirectories in the
// root (user-authored skills, junk) are ignored.
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

// Put writes the game to <root>/<name>/{SKILL.md,spec.star[,guide.md]}.
// SKILL.md is the rich auto-rendering (same shape export_game emits);
// spec.star is the canonical source; guide.md is the llm_guide body if
// non-empty (otherwise removed so a no-longer-relevant guide doesn't
// linger from a prior Put). created_at is preserved across Puts when
// the caller passes a zero time.
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

	// Parse the spec so we can render the rich SKILL.md. The source has
	// already been validated by RegisterUserGame, so this should never
	// fail in practice — but if it does, we surface the parse error
	// rather than write a junk file.
	spec, err := starlarkgame.LoadSpec(ug.Source)
	if err != nil {
		return fmt.Errorf("skills dir: render SKILL.md: %w", err)
	}
	skeleton := starlarkgame.BuildSkillSkeleton(spec, ug.LLMGuide, ug.UserID, created)

	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(skeleton.RenderMarkdown()), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "spec.star"), []byte(ug.Source), 0o644); err != nil {
		return err
	}

	guidePath := filepath.Join(dir, "guide.md")
	if ug.LLMGuide != "" {
		body := ug.LLMGuide
		if !strings.HasSuffix(body, "\n") {
			body += "\n"
		}
		if err := os.WriteFile(guidePath, []byte(body), 0o644); err != nil {
			return err
		}
	} else {
		// Empty llm_guide on this Put — clear any prior guide.md so reads
		// don't return stale content from before the author dropped their
		// notes.
		_ = os.Remove(guidePath)
	}
	return nil
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

// readGame reads <root>/<name>/{SKILL.md,spec.star} plus the optional
// guide.md. Returns os.ErrNotExist if SKILL.md or spec.star is missing.
//
// For back-compat with the v0.4–v0.5.1 skinny format (where llm_guide
// was embedded as the SKILL.md body), if guide.md is absent and the
// SKILL.md body doesn't start with a "# " heading the body is treated
// as the legacy llm_guide. The next Put migrates the layout to the
// three-file shape.
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

	var llmGuide string
	if g, err := os.ReadFile(filepath.Join(dir, "guide.md")); err == nil {
		llmGuide = strings.TrimRight(string(g), "\n")
	} else if !strings.HasPrefix(strings.TrimLeft(skill.Body, "\n"), "# ") {
		// Legacy skinny SKILL.md — body IS the llm_guide.
		body := strings.TrimRight(skill.Body, "\n")
		// Drop the placeholder line written when llm_guide was empty.
		if body != "(No strategy notes were authored for this game.)" {
			llmGuide = body
		}
	}

	return &UserGame{
		UserID:    skill.Owner,
		Name:      skill.Name,
		Source:    string(src),
		LLMGuide:  llmGuide,
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

