package mcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSkillsDirUserGames_PutGetListDelete(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenSkillsDirUserGames(dir)
	if err != nil {
		t.Fatalf("OpenSkillsDirUserGames: %v", err)
	}
	ctx := context.Background()

	const guide = "Play patiently. Three in a row LOSES."
	const source = "META = {\"name\": \"hex\"}\n"
	if err := s.Put(ctx, UserGame{UserID: "local", Name: "hex", Source: source, LLMGuide: guide}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// Files on disk look right.
	for _, f := range []string{"SKILL.md", "spec.star"} {
		if _, err := os.Stat(filepath.Join(dir, "hex", f)); err != nil {
			t.Errorf("missing %s: %v", f, err)
		}
	}
	skillBytes, _ := os.ReadFile(filepath.Join(dir, "hex", "SKILL.md"))
	skill := string(skillBytes)
	for _, want := range []string{"name: hex", "owner: local", "Play patiently", "---"} {
		if !strings.Contains(skill, want) {
			t.Errorf("SKILL.md missing %q; got:\n%s", want, skill)
		}
	}

	got, err := s.Get(ctx, "local", "hex")
	if err != nil || got == nil {
		t.Fatalf("Get: %v %v", got, err)
	}
	if got.Source != source || !strings.Contains(got.LLMGuide, "Play patiently") {
		t.Errorf("Get returned wrong content: source=%q guide=%q", got.Source, got.LLMGuide)
	}
	if got.CreatedAt.IsZero() {
		t.Errorf("CreatedAt not populated from frontmatter")
	}

	names, err := s.List(ctx, "local")
	if err != nil || len(names) != 1 || names[0] != "hex" {
		t.Errorf("List: %v %v", names, err)
	}

	all, err := s.ListAll(ctx)
	if err != nil || len(all) != 1 {
		t.Errorf("ListAll: %v %v", all, err)
	}

	if err := s.Delete(ctx, "local", "hex"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "hex")); !os.IsNotExist(err) {
		t.Errorf("game dir not removed after Delete (err=%v)", err)
	}
	got, _ = s.Get(ctx, "local", "hex")
	if got != nil {
		t.Errorf("expected nil after Delete, got %v", got)
	}
}

func TestSkillsDirUserGames_PreservesCreatedAtOnRePut(t *testing.T) {
	dir := t.TempDir()
	s, _ := OpenSkillsDirUserGames(dir)
	ctx := context.Background()

	_ = s.Put(ctx, UserGame{UserID: "local", Name: "g", Source: "v1\n"})
	first, _ := s.Get(ctx, "local", "g")
	t0 := first.CreatedAt

	// Re-Put with new source; CreatedAt should survive.
	if err := s.Put(ctx, UserGame{UserID: "local", Name: "g", Source: "v2\n"}); err != nil {
		t.Fatalf("re-Put: %v", err)
	}
	second, _ := s.Get(ctx, "local", "g")
	if !second.CreatedAt.Equal(t0) {
		t.Errorf("CreatedAt drifted on re-Put: %v -> %v", t0, second.CreatedAt)
	}
	if second.Source != "v2\n" {
		t.Errorf("Source not updated on re-Put: %q", second.Source)
	}
}

func TestSkillsDirUserGames_OwnerScoping(t *testing.T) {
	dir := t.TempDir()
	s, _ := OpenSkillsDirUserGames(dir)
	ctx := context.Background()

	_ = s.Put(ctx, UserGame{UserID: "alice", Name: "alpha", Source: "x"})
	_ = s.Put(ctx, UserGame{UserID: "bob", Name: "beta", Source: "y"})

	aliceList, _ := s.List(ctx, "alice")
	if len(aliceList) != 1 || aliceList[0] != "alpha" {
		t.Errorf("alice list = %v, want [alpha]", aliceList)
	}
	bobList, _ := s.List(ctx, "bob")
	if len(bobList) != 1 || bobList[0] != "beta" {
		t.Errorf("bob list = %v, want [beta]", bobList)
	}

	// Cross-owner Get returns nil.
	if v, _ := s.Get(ctx, "bob", "alpha"); v != nil {
		t.Errorf("bob got alice's game: %v", v)
	}
	// Cross-owner Delete is a no-op.
	if err := s.Delete(ctx, "bob", "alpha"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "alpha")); err != nil {
		t.Errorf("alice's game was deleted by bob's Delete: %v", err)
	}
}

func TestSkillsDirUserGames_IgnoresUnrelatedDirectories(t *testing.T) {
	dir := t.TempDir()
	// A random skill the user (or another tool) dropped here.
	_ = os.MkdirAll(filepath.Join(dir, "random-skill"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "random-skill", "SKILL.md"), []byte("# not ours\n"), 0o644)

	s, _ := OpenSkillsDirUserGames(dir)
	_ = s.Put(context.Background(), UserGame{UserID: "local", Name: "real", Source: "ok"})

	all, _ := s.ListAll(context.Background())
	if len(all) != 1 || all[0].Name != "real" {
		t.Errorf("scan picked up a non-game dir: %v", all)
	}
}
