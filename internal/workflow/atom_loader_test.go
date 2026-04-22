package workflow

import (
	"strings"
	"testing"
)

// TestLoadAtomBodyRendered_WriterCanonicalTreeRenders guards the writer
// canonical-output-tree atom's template rendering. Post-Cx-WRITER-SCOPE-
// REDUCTION the writer no longer authors env README files, so the atom
// no longer references `{{.EnvFolders}}` — the rendering contract now
// is: a live plan resolves every template expression; ProjectRoot +
// Slug are expanded; zero `{{` / `}}` markers survive the render.
func TestLoadAtomBodyRendered_WriterCanonicalTreeRenders(t *testing.T) {
	t.Parallel()
	plan := &RecipePlan{
		Framework: "nestjs",
		Tier:      "showcase",
		Slug:      "nestjs-showcase",
		Targets: []RecipeTarget{
			{Hostname: "api", Type: "nodejs@22"},
			{Hostname: "app", Type: "nodejs@22"},
		},
	}
	ctx := RenderContextFromPlan(plan, "")
	body, err := LoadAtomBodyRendered("briefs.writer.canonical-output-tree", ctx)
	if err != nil {
		t.Fatalf("LoadAtomBodyRendered: %v", err)
	}
	if strings.Contains(body, "{{") || strings.Contains(body, "}}") {
		t.Errorf("body contains unresolved template syntax:\n%s", body)
	}
	// ProjectRoot substitution: the per-codebase README path renders under /var/www.
	if !strings.Contains(body, "/var/www/") {
		t.Errorf("body missing rendered /var/www ProjectRoot prefix; got:\n%s", body)
	}
	// Slug substitution: the manifest path carries the live plan's slug.
	if !strings.Contains(body, "/var/www/zcprecipator/nestjs-showcase/ZCP_CONTENT_MANIFEST.json") {
		t.Errorf("body missing rendered manifest path with slug; got:\n%s", body)
	}
}

// TestLoadAtomBodyRendered_NilPlanReturnsRaw covers the fallback: when
// no active plan is supplied, the loader MUST return the raw atom
// body unchanged. Pre-session main-agent debug fetches rely on this.
func TestLoadAtomBodyRendered_NilPlanReturnsRaw(t *testing.T) {
	t.Parallel()
	ctx := RenderContextFromPlan(nil, "")
	body, err := LoadAtomBodyRendered("briefs.writer.canonical-output-tree", ctx)
	if err != nil {
		t.Fatalf("LoadAtomBodyRendered: %v", err)
	}
	// The raw atom has `{{` markers; with an empty context the render
	// would error or leave them unresolved. The helper returns raw.
	if !strings.Contains(body, "{{") {
		t.Errorf("nil-plan render should leave raw template syntax intact; got:\n%s", body)
	}
}

// TestCanonicalEnvFolders_OrderedSixEntries guards the exported helper
// used by tools/analyze + render context construction.
func TestCanonicalEnvFolders_OrderedSixEntries(t *testing.T) {
	t.Parallel()
	folders := CanonicalEnvFolders()
	if len(folders) != 6 {
		t.Fatalf("len=%d want=6", len(folders))
	}
	if folders[0] != "0 \u2014 AI Agent" {
		t.Errorf("folders[0]=%q want=%q", folders[0], "0 \u2014 AI Agent")
	}
	if folders[5] != "5 \u2014 Highly-available Production" {
		t.Errorf("folders[5]=%q want=%q", folders[5], "5 \u2014 Highly-available Production")
	}
}
