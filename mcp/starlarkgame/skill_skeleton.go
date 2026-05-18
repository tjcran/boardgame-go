package starlarkgame

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// SkillSkeleton is the mechanical part of a SKILL.md the server can
// generate for a registered game: YAML frontmatter, an auto-rendered
// "Moves" reference table from the spec's MOVES dict, and a verbatim
// inclusion of the designer's llm_guide. Strategy prose, UI notes, and
// other author-supplied content are out of scope — the skeleton ends
// with placeholder headings the author fills in.
type SkillSkeleton struct {
	Name        string
	Description string
	Owner       string // empty for built-ins
	MinPlayers  int
	MaxPlayers  int
	CreatedAt   time.Time
	LLMGuide    string

	// Moves is one entry per MOVES dict entry, ordered alphabetically by
	// move name for stable output.
	Moves []SkeletonMove
}

// SkeletonMove is the subset of a Move that goes into the rendered
// SKILL.md — name and declared positional argument shapes.
type SkeletonMove struct {
	Name string
	Args []ArgDef
}

// BuildSkillSkeleton synthesises a SkillSkeleton from a compiled Spec
// plus the designer-authored llm_guide (may be empty) and an optional
// owner ID (empty for built-ins or anonymous exports).
func BuildSkillSkeleton(s *Spec, llmGuide, owner string, createdAt time.Time) SkillSkeleton {
	moves := make([]SkeletonMove, 0, len(s.Moves))
	for _, mv := range s.Moves {
		moves = append(moves, SkeletonMove{Name: mv.Name, Args: append([]ArgDef(nil), mv.ArgsDef...)})
	}
	sort.Slice(moves, func(i, j int) bool { return moves[i].Name < moves[j].Name })

	return SkillSkeleton{
		Name:        s.Meta.Name,
		Description: s.Meta.Description,
		Owner:       owner,
		MinPlayers:  s.Meta.MinPlayers,
		MaxPlayers:  s.Meta.MaxPlayers,
		CreatedAt:   createdAt,
		LLMGuide:    llmGuide,
		Moves:       moves,
	}
}

// RenderMarkdown returns the SKILL.md text for the skeleton. Frontmatter
// is the same shape the skills-on-disk store writes at registration
// time, so a SKILL.md produced here can be dropped directly into
// $HOME/.claude/skills/games/<name>/.
func (sk SkillSkeleton) RenderMarkdown() string {
	var b strings.Builder

	b.WriteString("---\n")
	b.WriteString("name: " + sk.Name + "\n")
	if sk.Description != "" {
		b.WriteString("description: " + sk.Description + "\n")
	}
	if sk.Owner != "" {
		b.WriteString("owner: " + sk.Owner + "\n")
	}
	if !sk.CreatedAt.IsZero() {
		b.WriteString("created_at: " + sk.CreatedAt.UTC().Format(time.RFC3339) + "\n")
	}
	b.WriteString("---\n\n")

	if sk.Description != "" {
		b.WriteString("# " + sk.Name + "\n\n")
		b.WriteString(sk.Description + "\n\n")
	} else {
		b.WriteString("# " + sk.Name + "\n\n")
	}

	fmt.Fprintf(&b, "**Players:** %d–%d.\n\n", sk.MinPlayers, sk.MaxPlayers)

	b.WriteString("## Moves\n\n")
	if len(sk.Moves) == 0 {
		b.WriteString("_(none declared)_\n\n")
	} else {
		for _, m := range sk.Moves {
			b.WriteString("- `" + m.Name + "(")
			parts := make([]string, 0, len(m.Args))
			for _, a := range m.Args {
				parts = append(parts, renderArgSig(a))
			}
			b.WriteString(strings.Join(parts, ", "))
			b.WriteString(")`\n")
		}
		b.WriteString("\n")
	}

	if sk.LLMGuide != "" {
		b.WriteString("## Designer's notes\n\n")
		b.WriteString(strings.TrimRight(sk.LLMGuide, "\n"))
		b.WriteString("\n\n")
	}

	b.WriteString("## Strategy\n\n")
	b.WriteString("_(Fill in as you discover good play.)_\n")

	return b.String()
}

func renderArgSig(a ArgDef) string {
	out := a.Name
	if a.Type != "" {
		out += ": " + a.Type
	}
	if a.Min != nil && a.Max != nil {
		out += fmt.Sprintf(" [%d..%d]", *a.Min, *a.Max)
	} else if a.Min != nil {
		out += fmt.Sprintf(" [≥%d]", *a.Min)
	} else if a.Max != nil {
		out += fmt.Sprintf(" [≤%d]", *a.Max)
	}
	return out
}
