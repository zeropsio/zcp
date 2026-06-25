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

// TestGuidedSkillContent_ArchitectureGuardrails pins the compact
// architecture-quality layer of guided mode. These markers keep the skill from
// drifting back to service-picking without the small decision memory that makes
// later slices faster and safer.
func TestGuidedSkillContent_ArchitectureGuardrails(t *testing.T) {
	t.Parallel()
	files, err := ReadGuidedSkillTree()
	if err != nil {
		t.Fatalf("ReadGuidedSkillTree: %v", err)
	}

	got := make(map[string]string, len(files))
	for _, f := range files {
		got[f.RelPath] = f.Content
	}

	checks := map[string][]string{
		"SKILL.md": {
			"Architecture delta",
			"D1/D2/D3/D5",
		},
		"phases/align.md": {
			"Architecture drivers",
			"logical components",
			"collapsed modular app",
		},
		"phases/prd.md": {
			"Architecture drivers",
			"Architecture decisions",
			"Boundaries",
		},
		"phases/slices.md": {
			"## Preserve",
			"## Design seam",
			"Parallelization audit",
		},
		"phases/develop.md": {
			"Boundary",
			"Parallel sibling builds",
		},
		"phases/review-deploy.md": {
			"Delayed effects",
			"max 3",
		},
	}

	for rel, needles := range checks {
		content, ok := got[rel]
		if !ok {
			t.Fatalf("guided skill subtree missing %q", rel)
		}
		for _, needle := range needles {
			if !strings.Contains(content, needle) {
				t.Errorf("%s missing architecture guardrail marker %q", rel, needle)
			}
		}
	}
}

// TestGuidedSkillContent_DesignDimension pins the design/UX dimension threaded
// through the guided lifecycle (docs/spec-guided-mode.md §6.5). The instructions
// only TRIGGER the model's own design knowledge (which kit fits which stack, what
// makes a UI feel premium) — they must NOT enumerate kits or craft values, which
// the no-version lint in TestGuidedSkillContent_Invariants already guards. These
// markers pin that the trigger plus the structural enforcement seams (surface
// type per slice, UX-states-as-acceptance, the looks-right review angle) exist.
func TestGuidedSkillContent_DesignDimension(t *testing.T) {
	t.Parallel()
	files, err := ReadGuidedSkillTree()
	if err != nil {
		t.Fatalf("ReadGuidedSkillTree: %v", err)
	}

	got := make(map[string]string, len(files))
	for _, f := range files {
		got[f.RelPath] = f.Content
	}

	checks := map[string][]string{
		"SKILL.md":                {"look and feel right"},
		"phases/align.md":         {"surface type"},
		"phases/prd.md":           {"design chapter"},
		"phases/slices.md":        {"surface type"},
		"phases/develop.md":       {"Build to the established look"},
		"phases/review-deploy.md": {"looks-right"},
	}

	for rel, needles := range checks {
		content, ok := got[rel]
		if !ok {
			t.Fatalf("guided skill subtree missing %q", rel)
		}
		for _, needle := range needles {
			if !strings.Contains(content, needle) {
				t.Errorf("%s missing design-dimension marker %q", rel, needle)
			}
		}
	}
}

// TestGuidedSkillContent_RecipeFirst pins the recipe-first doctrine
// (docs/spec-guided-mode.md §6.7 / G8): guided's primary topology move is to
// anchor on the framework-matching curated recipe and provision it via
// route=recipe, with the decision set still governing (framework + grade +
// floor-service gates). align.md owns the doctrine; slices.md executes it via
// route=recipe. Without these markers a prose pass could silently drop
// recipe-first and the lifecycle would regress to from-scratch topology.
func TestGuidedSkillContent_RecipeFirst(t *testing.T) {
	t.Parallel()
	files, err := ReadGuidedSkillTree()
	if err != nil {
		t.Fatalf("ReadGuidedSkillTree: %v", err)
	}

	got := make(map[string]string, len(files))
	for _, f := range files {
		got[f.RelPath] = f.Content
	}

	checks := map[string][]string{
		"phases/align.md":  {"recipe fit", "Framework gate", "smallest-covering"},
		"phases/slices.md": {"route=recipe"},
	}

	for rel, needles := range checks {
		content, ok := got[rel]
		if !ok {
			t.Fatalf("guided skill subtree missing %q", rel)
		}
		for _, needle := range needles {
			if !strings.Contains(content, needle) {
				t.Errorf("%s missing recipe-first marker %q", rel, needle)
			}
		}
	}
}

// TestGuidedSkillContent_SeamIsTestSurface pins the by-design test discipline
// (docs/spec-guided-mode.md §6.6): a slice's seam is ONE interface — the boundary
// the build subagent works behind IS the surface the acceptance test crosses (the
// codebase-design model of a deep module tested through its own interface). So there
// is no separate `## Test seam` slice field, and develop.md frames the acceptance
// test as crossing that interface ("test surface"). This is what makes a
// test-author/implementer split unnecessary: the contract is the designed interface,
// not a hand-off artifact — so tautological / rubber-stamp / wrong-seam tests are
// dissolved by the module's shape, not patched by a guard rule.
func TestGuidedSkillContent_SeamIsTestSurface(t *testing.T) {
	t.Parallel()
	files, err := ReadGuidedSkillTree()
	if err != nil {
		t.Fatalf("ReadGuidedSkillTree: %v", err)
	}
	got := make(map[string]string, len(files))
	for _, f := range files {
		got[f.RelPath] = f.Content
	}
	if !strings.Contains(got["phases/develop.md"], "test surface") {
		t.Errorf("phases/develop.md must frame the slice seam as the test surface (the interface tests cross, not the functions behind it)")
	}
	if strings.Contains(got["phases/slices.md"], "## Test seam") {
		t.Errorf("phases/slices.md still carries a separate `## Test seam` field — the seam is one interface (build boundary == test surface); fold it into `## Design seam`")
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
