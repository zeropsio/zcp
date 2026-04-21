package workflow

import (
	"strings"
	"testing"
)

// TestLoadAtomBodyRendered_EnvFoldersResolved is the Cx-ENVFOLDERS-WIRED
// RED→GREEN test: loading an atom that references {{.EnvFolders}} +
// {{.ProjectRoot}} must produce a body with every template expression
// resolved. The v36 F-9 defect happened because dispatch-brief-atom
// returned the atom's raw bytes — `{{index .EnvFolders i}}` fragments
// reached the writer dispatch prompt verbatim and the main agent
// invented ghost slug names instead of the canonical numbered folders.
//
// Acceptance: the rendered body contains the first canonical env path
// AND no unresolved `{{` / `}}` tokens remain anywhere in the body.
func TestLoadAtomBodyRendered_EnvFoldersResolved(t *testing.T) {
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
	wantPath := "0 \u2014 AI Agent/README.md"
	if !strings.Contains(body, wantPath) {
		t.Errorf("body missing canonical env path %q; got:\n%s", wantPath, body)
	}
	// ProjectRoot substitution: every env path renders under /var/www.
	if !strings.Contains(body, "/var/www/environments/0 \u2014 AI Agent/") {
		t.Errorf("body missing rendered /var/www prefix for env 0; got:\n%s", body)
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
