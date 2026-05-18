package mcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// validSkillsSrc is a minimal but compileable Starlark spec the skills-on-disk
// tests can pass through LoadSpec without triggering a parse error in Put.
const validSkillsSrc = `META = {"name":"hex","min_players":2,"max_players":2,"description":"A toy."}
def setup(ctx): return {"x": 0}
def _go(state, ctx): return state
MOVES = {"go": {"args":[], "apply": _go}}
def end_if(state, ctx): return None
def legal_moves(state, ctx): return [{"move":"go","args":[]}]
`

func TestSkillsDirUserGames_PutWritesRichSkillAndSplitGuide(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenSkillsDirUserGames(dir)
	if err != nil {
		t.Fatalf("OpenSkillsDirUserGames: %v", err)
	}
	ctx := context.Background()

	const guide = "Play patiently. Three in a row LOSES."
	if err := s.Put(ctx, UserGame{UserID: "local", Name: "hex", Source: validSkillsSrc, LLMGuide: guide}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// Three files on disk.
	for _, f := range []string{"SKILL.md", "spec.star", "guide.md"} {
		if _, err := os.Stat(filepath.Join(dir, "hex", f)); err != nil {
			t.Errorf("missing %s: %v", f, err)
		}
	}

	// SKILL.md is the rich render — frontmatter + moves table + sections.
	skill, _ := os.ReadFile(filepath.Join(dir, "hex", "SKILL.md"))
	skillS := string(skill)
	for _, want := range []string{
		"name: hex", "owner: local",
		"# hex", "**Players:** 2–2.",
		"## Moves", "`go()`",
		"## Designer's notes", "Play patiently",
		"## Strategy",
	} {
		if !strings.Contains(skillS, want) {
			t.Errorf("SKILL.md missing %q; got:\n%s", want, skillS)
		}
	}

	// spec.star is the canonical source.
	src, _ := os.ReadFile(filepath.Join(dir, "hex", "spec.star"))
	if string(src) != validSkillsSrc {
		t.Errorf("spec.star drift: got %q", string(src))
	}

	// guide.md is the llm_guide body (with trailing newline added).
	gb, _ := os.ReadFile(filepath.Join(dir, "hex", "guide.md"))
	if got := strings.TrimRight(string(gb), "\n"); got != guide {
		t.Errorf("guide.md = %q, want %q", got, guide)
	}

	// Get round-trips llm_guide via guide.md.
	got, err := s.Get(ctx, "local", "hex")
	if err != nil || got == nil {
		t.Fatalf("Get: %v %v", got, err)
	}
	if got.Source != validSkillsSrc || got.LLMGuide != guide {
		t.Errorf("round-trip: source=%q guide=%q", got.Source, got.LLMGuide)
	}
	if got.CreatedAt.IsZero() {
		t.Errorf("CreatedAt not populated from frontmatter")
	}

	if err := s.Delete(ctx, "local", "hex"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "hex")); !os.IsNotExist(err) {
		t.Errorf("game dir not removed after Delete (err=%v)", err)
	}
}

func TestSkillsDirUserGames_PutWithEmptyGuide_OmitsAndCleansUp(t *testing.T) {
	dir := t.TempDir()
	s, _ := OpenSkillsDirUserGames(dir)
	ctx := context.Background()

	// First Put with a guide — guide.md exists.
	_ = s.Put(ctx, UserGame{UserID: "local", Name: "x", Source: validSkillsSrc, LLMGuide: "early notes"})
	if _, err := os.Stat(filepath.Join(dir, "x", "guide.md")); err != nil {
		t.Fatalf("guide.md should exist after Put with guide: %v", err)
	}

	// Re-Put with empty guide — guide.md is removed.
	_ = s.Put(ctx, UserGame{UserID: "local", Name: "x", Source: validSkillsSrc, LLMGuide: ""})
	if _, err := os.Stat(filepath.Join(dir, "x", "guide.md")); !os.IsNotExist(err) {
		t.Errorf("guide.md should be removed on re-Put with empty LLMGuide (err=%v)", err)
	}

	got, _ := s.Get(ctx, "local", "x")
	if got.LLMGuide != "" {
		t.Errorf("LLMGuide should be empty after re-Put cleanup, got %q", got.LLMGuide)
	}
}

func TestSkillsDirUserGames_LegacySkinnyFormat_ReadsLLMGuideFromBody(t *testing.T) {
	// Hand-write a v0.4-style SKILL.md (skinny: body IS the llm_guide,
	// no rich heading). The reader's back-compat path should pick it up.
	dir := t.TempDir()
	gameDir := filepath.Join(dir, "legacy")
	_ = os.MkdirAll(gameDir, 0o755)
	legacySkill := `---
name: legacy
owner: local
created_at: 2026-04-01T00:00:00Z
---

These are the strategy notes I wrote with v0.4.
`
	_ = os.WriteFile(filepath.Join(gameDir, "SKILL.md"), []byte(legacySkill), 0o644)
	_ = os.WriteFile(filepath.Join(gameDir, "spec.star"), []byte(validSkillsSrc), 0o644)

	s, _ := OpenSkillsDirUserGames(dir)
	got, err := s.Get(context.Background(), "local", "legacy")
	if err != nil || got == nil {
		t.Fatalf("Get legacy: %v %v", got, err)
	}
	if !strings.Contains(got.LLMGuide, "These are the strategy notes") {
		t.Errorf("legacy llm_guide not extracted from body; got %q", got.LLMGuide)
	}
}

func TestSkillsDirUserGames_PreservesCreatedAtOnRePut(t *testing.T) {
	dir := t.TempDir()
	s, _ := OpenSkillsDirUserGames(dir)
	ctx := context.Background()

	_ = s.Put(ctx, UserGame{UserID: "local", Name: "g", Source: validSkillsSrc})
	first, _ := s.Get(ctx, "local", "g")
	t0 := first.CreatedAt

	// Re-Put with a different source-shape; CreatedAt should survive.
	alt := strings.Replace(validSkillsSrc, `"description":"A toy."`, `"description":"Updated."`, 1)
	if err := s.Put(ctx, UserGame{UserID: "local", Name: "g", Source: alt}); err != nil {
		t.Fatalf("re-Put: %v", err)
	}
	second, _ := s.Get(ctx, "local", "g")
	if !second.CreatedAt.Equal(t0) {
		t.Errorf("CreatedAt drifted on re-Put: %v -> %v", t0, second.CreatedAt)
	}
	if !strings.Contains(second.Source, "Updated.") {
		t.Errorf("Source not updated on re-Put")
	}
}

func TestSkillsDirUserGames_OwnerScoping(t *testing.T) {
	dir := t.TempDir()
	s, _ := OpenSkillsDirUserGames(dir)
	ctx := context.Background()

	aliceSrc := strings.Replace(validSkillsSrc, `"name":"hex"`, `"name":"alpha"`, 1)
	bobSrc := strings.Replace(validSkillsSrc, `"name":"hex"`, `"name":"beta"`, 1)
	_ = s.Put(ctx, UserGame{UserID: "alice", Name: "alpha", Source: aliceSrc})
	_ = s.Put(ctx, UserGame{UserID: "bob", Name: "beta", Source: bobSrc})

	aliceList, _ := s.List(ctx, "alice")
	if len(aliceList) != 1 || aliceList[0] != "alpha" {
		t.Errorf("alice list = %v, want [alpha]", aliceList)
	}
	bobList, _ := s.List(ctx, "bob")
	if len(bobList) != 1 || bobList[0] != "beta" {
		t.Errorf("bob list = %v, want [beta]", bobList)
	}

	if v, _ := s.Get(ctx, "bob", "alpha"); v != nil {
		t.Errorf("bob got alice's game: %v", v)
	}
	if err := s.Delete(ctx, "bob", "alpha"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "alpha")); err != nil {
		t.Errorf("alice's game was deleted by bob's Delete: %v", err)
	}
}

func TestSkillsDirUserGames_IgnoresUnrelatedDirectories(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "random-skill"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "random-skill", "SKILL.md"), []byte("# not ours\n"), 0o644)

	s, _ := OpenSkillsDirUserGames(dir)
	realSrc := strings.Replace(validSkillsSrc, `"name":"hex"`, `"name":"real"`, 1)
	_ = s.Put(context.Background(), UserGame{UserID: "local", Name: "real", Source: realSrc})

	all, _ := s.ListAll(context.Background())
	if len(all) != 1 || all[0].Name != "real" {
		t.Errorf("scan picked up a non-game dir: %v", all)
	}
}

func TestSkillsDirUserGames_PutRejectsInvalidSpec(t *testing.T) {
	// The renderer parses the spec; an invalid source must be reported
	// rather than silently writing a junk SKILL.md.
	dir := t.TempDir()
	s, _ := OpenSkillsDirUserGames(dir)
	err := s.Put(context.Background(), UserGame{UserID: "local", Name: "bad", Source: "not starlark"})
	if err == nil {
		t.Fatalf("expected error rendering invalid spec")
	}
	if !strings.Contains(err.Error(), "render SKILL.md") {
		t.Errorf("error should mention render path; got %v", err)
	}
}
