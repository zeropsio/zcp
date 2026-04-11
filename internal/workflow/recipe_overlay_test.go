package workflow

import (
	"os"
	"path/filepath"
	"testing"
)

// TestOverlayRealREADMEs_OverlaysValidSingleRuntime verifies the baseline
// case: a single-runtime plan with a valid README on the mount gets its
// scaffold replaced by the mount content.
func TestOverlayRealREADMEs_OverlaysValidSingleRuntime(t *testing.T) {
	// Not parallel — writes to filesystem paths under a fixed base.
	dir := t.TempDir()
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

	oldBase := recipeMountBaseOverride
	recipeMountBaseOverride = dir
	defer func() { recipeMountBaseOverride = oldBase }()

	if got := OverlayRealREADMEs(files, plan); got != 1 {
		t.Fatalf("expected 1 README overlaid, got %d", got)
	}
	if files["appdev/README.md"] != realREADME {
		t.Errorf("files[appdev/README.md] not overlaid:\ngot: %q", files["appdev/README.md"])
	}
}

// TestOverlayRealREADMEs_OverlaysDualRuntime verifies that a dual-runtime
// recipe with READMEs at both codebases overlays both files.
func TestOverlayRealREADMEs_OverlaysDualRuntime(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"appdev", "apidev"} {
		d := filepath.Join(dir, name)
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "README.md"), []byte(validOverlayREADME(name)), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	plan := &RecipePlan{
		Framework: "nestjs",
		Slug:      "nestjs-showcase",
		Targets: []RecipeTarget{
			{Hostname: "app", Type: "static", Role: "app"},
			{Hostname: "api", Type: "nodejs@22", Role: "api"},
			{Hostname: "worker", Type: "nodejs@22", IsWorker: true, SharesCodebaseWith: "api"},
			{Hostname: "db", Type: "postgresql@17"},
		},
	}
	files := map[string]string{
		"appdev/README.md": "# TODO stub\n",
		"apidev/README.md": "# TODO stub\n",
	}

	oldBase := recipeMountBaseOverride
	recipeMountBaseOverride = dir
	defer func() { recipeMountBaseOverride = oldBase }()

	if got := OverlayRealREADMEs(files, plan); got != 2 {
		t.Fatalf("expected 2 READMEs overlaid, got %d", got)
	}
	if files["appdev/README.md"] == "# TODO stub\n" {
		t.Error("appdev/README.md should have been overlaid")
	}
	if files["apidev/README.md"] == "# TODO stub\n" {
		t.Error("apidev/README.md should have been overlaid")
	}
}

// TestOverlayRealREADMEs_SkipsWorkers verifies that worker targets (regardless
// of SharesCodebaseWith) do not get per-target overlays. Shared-codebase
// workers use the host target's README; separate-codebase workers are not
// currently scaffolded by BuildFinalizeOutput.
func TestOverlayRealREADMEs_SkipsWorkers(t *testing.T) {
	dir := t.TempDir()
	workerDir := filepath.Join(dir, "workerdev")
	if err := os.MkdirAll(workerDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workerDir, "README.md"), []byte(validOverlayREADME("worker")), 0o600); err != nil {
		t.Fatal(err)
	}

	plan := &RecipePlan{
		Targets: []RecipeTarget{
			{Hostname: "worker", Type: "nodejs@22", IsWorker: true},
		},
	}
	files := map[string]string{}

	oldBase := recipeMountBaseOverride
	recipeMountBaseOverride = dir
	defer func() { recipeMountBaseOverride = oldBase }()

	if got := OverlayRealREADMEs(files, plan); got != 0 {
		t.Errorf("expected 0 overlays for worker-only plan, got %d", got)
	}
}

// TestOverlayRealREADMEs_RejectsTODOContent refuses to overlay README content
// that still has TODO scaffold markers.
func TestOverlayRealREADMEs_RejectsTODOContent(t *testing.T) {
	dir := t.TempDir()
	appdevDir := filepath.Join(dir, "appdev")
	if err := os.MkdirAll(appdevDir, 0o755); err != nil {
		t.Fatal(err)
	}
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

	if got := OverlayRealREADMEs(files, plan); got != 0 {
		t.Errorf("expected 0 overlays when README has TODO scaffold, got %d", got)
	}
	if files["appdev/README.md"] != "SCAFFOLD" {
		t.Error("files map should be unchanged when overlay refuses")
	}
}

// TestOverlayRealREADMEs_RejectsMissingMarkers refuses to overlay README
// content that is missing any of the required extract-fragment markers.
func TestOverlayRealREADMEs_RejectsMissingMarkers(t *testing.T) {
	dir := t.TempDir()
	appdevDir := filepath.Join(dir, "appdev")
	if err := os.MkdirAll(appdevDir, 0o755); err != nil {
		t.Fatal(err)
	}
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

	if got := OverlayRealREADMEs(files, plan); got != 0 {
		t.Errorf("expected 0 overlays when README missing markers, got %d", got)
	}
}

// TestOverlayRealREADMEs_NoFileNoOp returns 0 overlays when the mount has no
// README for the target at all.
func TestOverlayRealREADMEs_NoFileNoOp(t *testing.T) {
	plan := &RecipePlan{Targets: []RecipeTarget{{Hostname: "app", Type: "php-nginx@8.4"}}}
	files := map[string]string{"appdev/README.md": "SCAFFOLD"}

	oldBase := recipeMountBaseOverride
	recipeMountBaseOverride = "/nonexistent-path-for-test"
	defer func() { recipeMountBaseOverride = oldBase }()

	if got := OverlayRealREADMEs(files, plan); got != 0 {
		t.Errorf("expected 0 overlays when mount README is missing, got %d", got)
	}
	if files["appdev/README.md"] != "SCAFFOLD" {
		t.Error("files map should be unchanged when overlay does not apply")
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
