package workflow

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/content"
)

// sectionContent extracts a named <section>...</section> body from recipe.md.
func sectionContent(t *testing.T, name string) string {
	t.Helper()
	md, err := content.GetWorkflow("recipe")
	if err != nil {
		t.Fatalf("load recipe.md: %v", err)
	}
	s := ExtractSection(md, name)
	if s == "" {
		t.Fatalf("section %q not found in recipe.md", name)
	}
	return s
}

// recipeSectionNames are all top-level sections in recipe.md.
// Used by assertPresentIn to detect leaks of content into the wrong sections.
var recipeSectionNames = []string{
	"research-minimal",
	"research-showcase",
	"provision",
	"generate",
	"generate-fragments",
	"deploy",
	"finalize",
	"close",
}

// assertPresentIn asserts a string appears in exactly the named sections and
// nowhere else across the recipe.md catalogue.
func assertPresentIn(t *testing.T, needle string, sections ...string) {
	t.Helper()
	md, err := content.GetWorkflow("recipe")
	if err != nil {
		t.Fatalf("load recipe.md: %v", err)
	}
	wanted := make(map[string]struct{}, len(sections))
	for _, s := range sections {
		wanted[s] = struct{}{}
	}
	for _, s := range recipeSectionNames {
		body := ExtractSection(md, s)
		has := strings.Contains(body, needle)
		_, shouldHave := wanted[s]
		if shouldHave && !has {
			t.Errorf("needle %q: expected in section %q but missing", needle, s)
		}
		if !shouldHave && has {
			t.Errorf("needle %q: unexpected in section %q (should only appear in %v)", needle, s, sections)
		}
	}
}

// TestRecipe_PerRepoReadme asserts generate documents the per-codebase README
// requirement explicitly (LOG2 bugs 9, 12). Each codebase gets its own
// README with all 3 extract fragments.
func TestRecipe_PerRepoReadme(t *testing.T) {
	t.Parallel()

	generate := sectionContent(t, "generate")
	for _, p := range []string{"each codebase", "its own README", "all 3 extract fragments"} {
		if !strings.Contains(generate, p) {
			t.Errorf("generate missing per-repo README rule phrase %q", p)
		}
	}

	// The misleading singular-README wording must be gone.
	forbidden := "The API's README.md contains the integration guide"
	if strings.Contains(generate, forbidden) {
		t.Errorf("generate still contains misleading wording %q — caused LOG2 bugs 9, 12", forbidden)
	}
}

// TestRecipe_DevServerEnvVarRule asserts generate documents the dev-server
// runtime env var rule for Vite/webpack/Next dev servers (LOG2 bug 15).
// Client-side env vars must be in run.envVariables on setup: dev, not just
// build.envVariables.
func TestRecipe_DevServerEnvVarRule(t *testing.T) {
	t.Parallel()

	generate := sectionContent(t, "generate")
	for _, p := range []string{"dev server", "run.envVariables", "setup: dev"} {
		if !strings.Contains(generate, p) {
			t.Errorf("generate missing dev-server env var rule phrase %q", p)
		}
	}
}

// TestRecipe_SubAgentBriefPlacement asserts the sub-agent brief content moved
// into deploy in Phase 4, the generate-dashboard section is gone, and the
// execOnce trap + targetService warning + Vite collision warning all landed
// at deploy.
func TestRecipe_SubAgentBriefPlacement(t *testing.T) {
	t.Parallel()

	// Deploy's Step 4b sub-agent brief must carry the "where commands run" rule.
	if !strings.Contains(sectionContent(t, "deploy"), "Where app-level commands run") {
		t.Error("deploy Step 4b missing sub-agent brief 'Where app-level commands run' rule")
	}

	// generate-dashboard section should NOT exist any more — content moved to deploy.
	md, err := content.GetWorkflow("recipe")
	if err != nil {
		t.Fatalf("load recipe.md: %v", err)
	}
	if ExtractSection(md, "generate-dashboard") != "" {
		t.Error("generate-dashboard section should be removed in Phase 4 — content moves to deploy")
	}

	// generate section should still have the skeleton write checklist inline.
	if !strings.Contains(sectionContent(t, "generate"), "skeleton") {
		t.Error("generate must still have skeleton-write guidance inline (compressed from old generate-dashboard)")
	}

	// targetService parameter-name warning must be present at deploy (the
	// parameter itself appears in other sections' example commands — here we
	// specifically check the warning wording is present at deploy).
	deploy := sectionContent(t, "deploy")
	if !strings.Contains(deploy, "Parameter naming") || !strings.Contains(deploy, "`targetService` (NOT") {
		t.Error("deploy missing targetService parameter-name warning (LOG2 bug 4)")
	}

	// execOnce burn-on-failure trap must be documented at deploy.
	if !strings.Contains(deploy, "burn-on-failure") {
		t.Error("deploy missing zsc execOnce burn-on-failure trap warning")
	}

	// Vite port collision warning must be at deploy.
	if !strings.Contains(deploy, "already running") {
		t.Error("deploy missing Vite port-collision warning (LOG2 bug prevention)")
	}
}

// TestRecipe_ProvisionContent asserts that provision carries the content moved
// in by Phase 3 (git config, strengthened discover instruction) and that worker
// shape prose previously in research-showcase is gone.
func TestRecipe_ProvisionContent(t *testing.T) {
	t.Parallel()

	// Git safe.directory for SSHFS mount MUST be documented at provision.
	// (Deploy's sub-agent brief references the provision-set config, so the
	// substring is allowed to appear there as a back-reference too.)
	assertPresentIn(t, "safe.directory", "provision", "deploy")
	// The authoritative config commands must live at provision (Phase 3.4).
	if !strings.Contains(sectionContent(t, "provision"), `git config --global --add safe.directory`) {
		t.Error("provision must carry the authoritative safe.directory git config commands (Phase 3.4)")
	}

	// The zerops_discover instruction must be strengthened with an explicit
	// catalog/record step, not just "call this and use the output".
	provision := sectionContent(t, "provision")
	if !strings.Contains(provision, "catalog the output") && !strings.Contains(provision, "Catalog the output") {
		t.Error("provision must tell the agent to catalog zerops_discover output for reference at generate, not just call the tool")
	}

	// Managed service env var discovery must be at provision.
	assertPresentIn(t, "zerops_discover includeEnvs=true", "provision")
}

// TestRecipe_ResearchContent guards against research-step bloat and framework-
// hardcoding. research-minimal must drop form-field description prose the
// agent fills from training data; research-showcase must keep the principle-
// based classification and worker decision rules.
func TestRecipe_ResearchContent(t *testing.T) {
	t.Parallel()

	// Research should NOT contain form-field prose for fields the agent fills
	// from training data. The RecipeTarget / ResearchData jsonschema already
	// exposes field descriptions on the tool input.
	forbidden := []string{
		"**Package manager**",
		"**HTTP port**",
		"**Build commands**",
		"**Migration command**",
		"**Logging driver**",
		"**Needs app secret**",
		`zerops_knowledge recipe="{hello-world-slug}"`,
		"The research load is for filling the plan form",
	}
	minimal := sectionContent(t, "research-minimal")
	for _, needle := range forbidden {
		if strings.Contains(minimal, needle) {
			t.Errorf("research-minimal still contains forbidden string %q — should be removed in reshuffle", needle)
		}
	}

	// Research showcase must retain the classification rule and the 3-test
	// worker rule.
	required := []string{
		"Full-stack",
		"API-first",
		"sharesCodebaseWith",
		"framework's own bundled CLI",
		"independent dependency manifest",
	}
	showcase := sectionContent(t, "research-showcase")
	for _, needle := range required {
		if !strings.Contains(showcase, needle) {
			t.Errorf("research-showcase missing required content %q", needle)
		}
	}
}
