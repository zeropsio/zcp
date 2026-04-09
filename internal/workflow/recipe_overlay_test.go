package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOverlayRealAppREADME_OverlaysValid(t *testing.T) {
	// Not parallel — writes to filesystem paths under a fixed base.
	dir := t.TempDir()
	// Stand up a fake mount at dir/appdev so mountREADMEPathForPlan finds it.
	appdevDir := filepath.Join(dir, "appdev")
	if err := os.MkdirAll(appdevDir, 0o755); err != nil {
		t.Fatal(err)
	}
	realREADME := validOverlayREADME("app")
	if err := os.WriteFile(filepath.Join(appdevDir, "README.md"), []byte(realREADME), 0o600); err != nil {
		t.Fatal(err)
	}

	plan := &RecipePlan{
		Framework: "test",
		Slug:      "test-recipe",
		Targets: []RecipeTarget{
			{Hostname: "app", Type: "php-nginx@8.4"},
			{Hostname: "db", Type: "postgresql@17"},
		},
	}
	files := map[string]string{
		"appdev/README.md": "# TODO stub\n",
	}

	// Point mountREADMEPathForPlan at the temp dir via the override.
	oldBase := recipeMountBaseOverride
	recipeMountBaseOverride = dir
	defer func() { recipeMountBaseOverride = oldBase }()

	if !OverlayRealAppREADME(files, plan) {
		t.Fatal("expected overlay to apply")
	}
	if files["appdev/README.md"] != realREADME {
		t.Errorf("files[appdev/README.md] not overlaid:\ngot: %q", files["appdev/README.md"])
	}
}

func TestOverlayRealAppREADME_RejectsTODOContent(t *testing.T) {
	dir := t.TempDir()
	appdevDir := filepath.Join(dir, "appdev")
	if err := os.MkdirAll(appdevDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Content with valid markers but still has TODO scaffold text.
	content := validOverlayREADME("app") +
		"\n```yaml\nzerops:\n  # TODO: paste the full zerops.yaml content here\n```\n"
	if err := os.WriteFile(filepath.Join(appdevDir, "README.md"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	plan := &RecipePlan{Targets: []RecipeTarget{{Hostname: "app", Type: "php-nginx@8.4"}}}
	files := map[string]string{"appdev/README.md": "SCAFFOLD"}

	oldBase := recipeMountBaseOverride
	recipeMountBaseOverride = dir
	defer func() { recipeMountBaseOverride = oldBase }()

	if OverlayRealAppREADME(files, plan) {
		t.Error("expected overlay to refuse content with TODO scaffold markers")
	}
	if files["appdev/README.md"] != "SCAFFOLD" {
		t.Error("files map should be unchanged when overlay refuses")
	}
}

func TestOverlayRealAppREADME_RejectsMissingMarkers(t *testing.T) {
	dir := t.TempDir()
	appdevDir := filepath.Join(dir, "appdev")
	if err := os.MkdirAll(appdevDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Missing knowledge-base marker.
	content := `# App
<!-- #ZEROPS_EXTRACT_START:intro# -->
x
<!-- #ZEROPS_EXTRACT_END:intro# -->
<!-- #ZEROPS_EXTRACT_START:integration-guide# -->
x
<!-- #ZEROPS_EXTRACT_END:integration-guide# -->
`
	if err := os.WriteFile(filepath.Join(appdevDir, "README.md"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	plan := &RecipePlan{Targets: []RecipeTarget{{Hostname: "app", Type: "php-nginx@8.4"}}}
	files := map[string]string{"appdev/README.md": "SCAFFOLD"}

	oldBase := recipeMountBaseOverride
	recipeMountBaseOverride = dir
	defer func() { recipeMountBaseOverride = oldBase }()

	if OverlayRealAppREADME(files, plan) {
		t.Error("expected overlay to refuse content missing required markers")
	}
}

func TestOverlayRealAppREADME_NoFileNoOp(t *testing.T) {
	// Not parallel — mutates recipeMountBaseOverride (package-level var).
	plan := &RecipePlan{Targets: []RecipeTarget{{Hostname: "app", Type: "php-nginx@8.4"}}}
	files := map[string]string{"appdev/README.md": "SCAFFOLD"}

	oldBase := recipeMountBaseOverride
	recipeMountBaseOverride = "/nonexistent-path-for-test"
	defer func() { recipeMountBaseOverride = oldBase }()

	if OverlayRealAppREADME(files, plan) {
		t.Error("expected no overlay when source file does not exist")
	}
	if files["appdev/README.md"] != "SCAFFOLD" {
		t.Error("files map should be unchanged when overlay does not apply")
	}
}

func TestOverlayRealAppREADME_SkipsWorkersAsAppTarget(t *testing.T) {
	// Not parallel — reads recipeMountBaseOverride (package-level var).
	plan := &RecipePlan{
		Targets: []RecipeTarget{
			{Hostname: "worker", Type: "nodejs@22", IsWorker: true},
			{Hostname: "app", Type: "nodejs@22"},
		},
	}
	// mountREADMEPathForPlan should select "app" (first non-worker runtime),
	// not "worker".
	got := mountREADMEPathForPlan(plan)
	wantSuffix := "/appdev/README.md"
	if got == "" || got[len(got)-len(wantSuffix):] != wantSuffix {
		t.Errorf("expected path ending in %q, got %q", wantSuffix, got)
	}
}

func TestOverlayRealAppREADME_PrefersAPITarget(t *testing.T) {
	// Not parallel — mutates recipeMountBaseOverride (package-level var).
	plan := &RecipePlan{
		Targets: []RecipeTarget{
			{Hostname: "app", Type: "static", Role: "app"},
			{Hostname: "api", Type: "nodejs@22", Role: "api"},
		},
	}
	// mountREADMEPathForPlan should select "api" (Role: "api"), not "app".
	got := mountREADMEPathForPlan(plan)
	wantSuffix := "/apidev/README.md"
	if got == "" || !strings.HasSuffix(got, wantSuffix) {
		t.Errorf("expected path ending in %q, got %q", wantSuffix, got)
	}
}

func TestOverlayRealAppREADME_FallsBackWithoutAPIRole(t *testing.T) {
	// Not parallel — reads recipeMountBaseOverride (package-level var).
	plan := &RecipePlan{
		Targets: []RecipeTarget{
			{Hostname: "app", Type: "nodejs@22"},
		},
	}
	got := mountREADMEPathForPlan(plan)
	wantSuffix := "/appdev/README.md"
	if got == "" || !strings.HasSuffix(got, wantSuffix) {
		t.Errorf("expected path ending in %q, got %q", wantSuffix, got)
	}
}

func validOverlayREADME(_ string) string {
	return `# Test Recipe App

<!-- #ZEROPS_EXTRACT_START:intro# -->
A minimal app.
<!-- #ZEROPS_EXTRACT_END:intro# -->

## Integration Guide

<!-- #ZEROPS_EXTRACT_START:integration-guide# -->

### 1. Adding zerops.yaml
At repo root.

<!-- #ZEROPS_EXTRACT_END:integration-guide# -->

<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->

### Gotchas
- One.

<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->
`
}
