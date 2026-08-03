package content

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// GuidedSkillDirsRel are the materialized guided-skill directories, relative
// to the project root — one per agent-discovery root, the same two roots every
// skill pack publishes into (spec-skill-packs.md §2). Guided is agent-neutral:
// Claude Code discovers its copy natively under .claude/skills/, and every
// other agent reads the neutral .agents/skills/ path the AGENTS.md guided
// block names. Writing only one root is what made guided Claude-Code-only.
//
// The skill is a SUBTREE (router SKILL.md + phases/*.md); ReadGuidedSkillTree
// enumerates the whole thing and `zcp init` writes it under each dir (and
// removes every dir when guided is off — the toggle).
var GuidedSkillDirsRel = []string{
	".agents/skills/guided",
	".claude/skills/guided",
}

// guidedSkillEmbedRoot is the embedded path of the guided-skill subtree.
const guidedSkillEmbedRoot = "templates/skills/guided"

// GuidedSkillFile is one file in the embedded guided-skill subtree, with its
// path relative to the skill root (e.g. "SKILL.md", "phases/align.md").
type GuidedSkillFile struct {
	RelPath string
	Content string
}

// ReadGuidedSkillTree returns every file in the embedded guided-skill subtree
// — the router SKILL.md plus the phases/*.md progressive-disclosure files —
// each path relative to the skill root, sorted for deterministic write order.
// The guided lifecycle is content-only; init materializes this whole subtree
// so the host can read the router and load each phase on demand.
func ReadGuidedSkillTree() ([]GuidedSkillFile, error) {
	var out []GuidedSkillFile
	walkErr := fs.WalkDir(templateFS, guidedSkillEmbedRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, readErr := templateFS.ReadFile(p)
		if readErr != nil {
			return fmt.Errorf("read guided skill file %s: %w", p, readErr)
		}
		rel := strings.TrimPrefix(strings.TrimPrefix(p, guidedSkillEmbedRoot), "/")
		out = append(out, GuidedSkillFile{RelPath: rel, Content: string(data)})
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("walk guided skill tree: %w", walkErr)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RelPath < out[j].RelPath })
	return out, nil
}

// guidedMarkerRel is the LOCAL per-project marker recording the user's
// guided-mode preference. It lives in .zcp/ (the gitignored project meta), so
// guided is a local preference for this checkout — not committed, not shared.
// `zcp init --guided` writes it; init + the serve-time refresh read it to
// decide whether to render the guided block.
const guidedMarkerRel = ".zcp/state/guided"

// GuidedEnabled reports whether guided mode is on for the project at root.
func GuidedEnabled(root string) bool {
	_, err := os.Stat(filepath.Join(root, filepath.FromSlash(guidedMarkerRel)))
	return err == nil
}

// SetGuided records (on) or clears (off) the local guided preference.
func SetGuided(root string, on bool) error {
	p := filepath.Join(root, filepath.FromSlash(guidedMarkerRel))
	if !on {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("clear guided marker: %w", err)
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return fmt.Errorf("mkdir state dir: %w", err)
	}
	if err := os.WriteFile(p, []byte("on\n"), 0o644); err != nil { //nolint:gosec // G306: local marker
		return fmt.Errorf("write guided marker: %w", err)
	}
	return nil
}
