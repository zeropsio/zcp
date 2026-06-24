package content

import (
	"regexp"
	"strings"
	"testing"
)

// TestReadGuidedSkillTree_RouterAndPhases pins the guided-skill subtree shape:
// the router SKILL.md plus the per-phase progressive-disclosure files. The
// lifecycle is content-only (docs/spec-guided-mode.md) — init materializes
// exactly this set, so the subtree IS the contract.
func TestReadGuidedSkillTree_RouterAndPhases(t *testing.T) {
	t.Parallel()
	files, err := ReadGuidedSkillTree()
	if err != nil {
		t.Fatalf("ReadGuidedSkillTree: %v", err)
	}

	got := make(map[string]string, len(files))
	for _, f := range files {
		if strings.TrimSpace(f.Content) == "" {
			t.Errorf("guided skill file %q is empty", f.RelPath)
		}
		got[f.RelPath] = f.Content
	}

	want := []string{
		"SKILL.md",
		"phases/align.md",
		"phases/prd.md",
		"phases/slices.md",
		"phases/develop.md",
		"phases/review-deploy.md",
		"phases/release.md",
	}
	for _, w := range want {
		if _, ok := got[w]; !ok {
			t.Errorf("guided skill subtree missing %q (have %d files)", w, len(files))
		}
	}
	if router := got["SKILL.md"]; !strings.Contains(router, "name: guided") {
		t.Errorf("SKILL.md missing the skill front-matter name:\n%s", router)
	}
}

// hardcodedServiceVersionRe matches a service@version token (e.g. postgresql@16,
// nodejs@22, go@1) — a hardcoded platform fact. Guided content must route every
// version to its live owner (zerops_knowledge / the active-filtered schema),
// never pin one. Mode variants (`:single` / `:ha`) carry no `@` and are fine.
var hardcodedServiceVersionRe = regexp.MustCompile(`[a-z][a-z0-9.+-]*@[0-9]`)

// TestGuidedSkillContent_Invariants pins the content disciplines the guided
// skill must hold (the P1 skills-lint gate, docs/spec-guided-mode.md):
//   - tools-only: a zerops:// reference appears only via the tool-call form
//     `zerops_knowledge uri="zerops://..."`, never as a bare backticked URI
//     (ZCP advertises no MCP resources protocol — a bare URI is dead bait);
//   - no hardcoded service versions (route to the owner instead).
func TestGuidedSkillContent_Invariants(t *testing.T) {
	t.Parallel()
	files, err := ReadGuidedSkillTree()
	if err != nil {
		t.Fatalf("ReadGuidedSkillTree: %v", err)
	}
	for _, f := range files {
		if strings.Contains(f.Content, "`zerops://") {
			t.Errorf("%s: bare backticked `zerops://` — use the tool-call form `zerops_knowledge uri=\"zerops://...\"` (ZCP is tools-only)", f.RelPath)
		}
		if m := hardcodedServiceVersionRe.FindString(f.Content); m != "" {
			t.Errorf("%s: hardcoded service version %q — route the version to its live owner, never pin it in content", f.RelPath, m)
		}
	}
}

// phaseRefRe matches a progressive-disclosure pointer to a phase file.
var phaseRefRe = regexp.MustCompile(`phases/[a-z-]+\.md`)

// TestGuidedSkillContent_PhaseReferencesResolve pins router↔phases coherence
// (tell == check for the progressive-disclosure structure): every phases/*.md
// the content points at must exist in the subtree (no dangling pointer), and
// every phase file in the subtree must be referenced (no orphan the router
// never routes to). Either drift ships a skill that points at a missing file
// or carries a phase the host never reads.
func TestGuidedSkillContent_PhaseReferencesResolve(t *testing.T) {
	t.Parallel()
	files, err := ReadGuidedSkillTree()
	if err != nil {
		t.Fatalf("ReadGuidedSkillTree: %v", err)
	}

	present := make(map[string]bool)    // phase files that exist
	referenced := make(map[string]bool) // phase files pointed at by some file
	for _, f := range files {
		if strings.HasPrefix(f.RelPath, "phases/") {
			present[f.RelPath] = true
		}
	}
	for _, f := range files {
		for _, ref := range phaseRefRe.FindAllString(f.Content, -1) {
			referenced[ref] = true
			if !present[ref] {
				t.Errorf("%s references %q which is not in the guided skill subtree (dangling pointer)", f.RelPath, ref)
			}
		}
	}
	for p := range present {
		if !referenced[p] {
			t.Errorf("phase file %q exists but is never referenced (orphan — the router never routes to it)", p)
		}
	}
}
