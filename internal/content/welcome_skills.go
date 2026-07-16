package content

import (
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

// welcomeSkillsEmbedRoot is the embedded path of the curated welcome-skills
// tree (docs/spec-welcome-mode.md §6 W-SKILLS): one directory per skill
// slug, each holding a single reviewed SKILL.md. Unlike the guided skill (a
// router + phases subtree, guided.go), each welcome skill is a flat,
// single-file skill — the per-slug subdirectory just keeps the embedded
// shape uniform with the .claude/skills/<slug>/ layout it installs into.
const welcomeSkillsEmbedRoot = "templates/welcome-skills"

// WelcomeSkillFile is one file in the embedded curated welcome-skills tree.
// Slug identifies which skill it belongs to; RelPath is its path relative to
// that skill's own root (today always "SKILL.md").
type WelcomeSkillFile struct {
	Slug    string
	RelPath string
	Content string
}

// ReadWelcomeSkillsTree returns every file in the embedded welcome-skills
// tree, sorted by (slug, relpath) for deterministic install order. Mirrors
// ReadGuidedSkillTree's walk-and-collect shape (guided.go).
func ReadWelcomeSkillsTree() ([]WelcomeSkillFile, error) {
	var out []WelcomeSkillFile
	walkErr := fs.WalkDir(templateFS, welcomeSkillsEmbedRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, readErr := templateFS.ReadFile(p)
		if readErr != nil {
			return fmt.Errorf("read welcome skill file %s: %w", p, readErr)
		}
		rel := strings.TrimPrefix(strings.TrimPrefix(p, welcomeSkillsEmbedRoot), "/")
		slug, relPath, ok := strings.Cut(rel, "/")
		if !ok {
			return fmt.Errorf("welcome skill file %q is not nested under a slug directory", rel)
		}
		out = append(out, WelcomeSkillFile{Slug: slug, RelPath: relPath, Content: string(data)})
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("walk welcome skills tree: %w", walkErr)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Slug != out[j].Slug {
			return out[i].Slug < out[j].Slug
		}
		return out[i].RelPath < out[j].RelPath
	})
	return out, nil
}
