package workflow

import (
	"os"
	"path/filepath"
	"strings"
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

// TestOverlayManifest_CopiesValidJSON is the Cx-MANIFEST-OVERLAY
// RED→GREEN test. v37 F-23: writer authored ZCP_CONTENT_MANIFEST.json
// at the recipe output root but it never reached the deliverable —
// only per-codebase files were staged. OverlayManifest closes the
// gap: read the writer's manifest from
// /var/www/zcprecipator/{slug}/ZCP_CONTENT_MANIFEST.json, confirm
// valid JSON, and stage it into the files map at the deliverable root.
func TestOverlayManifest_CopiesValidJSON(t *testing.T) {
	dir := t.TempDir()
	slug := "nestjs-showcase"
	recipeDir := filepath.Join(dir, "zcprecipator", slug)
	if err := os.MkdirAll(recipeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"facts":[{"title":"foo","classification":"intersection","routed_to":"content_gotcha"}]}`
	if err := os.WriteFile(filepath.Join(recipeDir, "ZCP_CONTENT_MANIFEST.json"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}

	plan := &RecipePlan{Slug: slug}
	files := map[string]string{}

	oldBase := recipeMountBaseOverride
	recipeMountBaseOverride = dir
	defer func() { recipeMountBaseOverride = oldBase }()

	if ok := OverlayManifest(files, plan); !ok {
		t.Fatal("expected OverlayManifest to return true on valid manifest")
	}
	if files["ZCP_CONTENT_MANIFEST.json"] != manifest {
		t.Errorf("files[ZCP_CONTENT_MANIFEST.json] = %q; want %q", files["ZCP_CONTENT_MANIFEST.json"], manifest)
	}
}

// TestOverlayManifest_SkipsInvalidJSON: malformed manifest JSON on the
// mount is never staged; the deliverable must never ship a broken
// manifest. Return value is false so the caller can log a helpful note.
func TestOverlayManifest_SkipsInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	slug := "bun-minimal"
	recipeDir := filepath.Join(dir, "zcprecipator", slug)
	if err := os.MkdirAll(recipeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(recipeDir, "ZCP_CONTENT_MANIFEST.json"), []byte("not json {"), 0o600); err != nil {
		t.Fatal(err)
	}

	plan := &RecipePlan{Slug: slug}
	files := map[string]string{}

	oldBase := recipeMountBaseOverride
	recipeMountBaseOverride = dir
	defer func() { recipeMountBaseOverride = oldBase }()

	if ok := OverlayManifest(files, plan); ok {
		t.Fatal("expected OverlayManifest to return false on malformed JSON")
	}
	if _, exists := files["ZCP_CONTENT_MANIFEST.json"]; exists {
		t.Error("files map must not carry the malformed manifest")
	}
}

// TestOverlayManifest_MissingFileSilentSkip: if the writer never wrote
// the manifest, OverlayManifest reports false and leaves the files map
// untouched. Callers log their own "manifest not authored" note.
func TestOverlayManifest_MissingFileSilentSkip(t *testing.T) {
	dir := t.TempDir()
	plan := &RecipePlan{Slug: "no-such-slug"}
	files := map[string]string{}

	oldBase := recipeMountBaseOverride
	recipeMountBaseOverride = dir
	defer func() { recipeMountBaseOverride = oldBase }()

	if ok := OverlayManifest(files, plan); ok {
		t.Error("expected OverlayManifest to return false when manifest missing")
	}
	if len(files) != 0 {
		t.Errorf("files map should stay empty; got %v", files)
	}
}

// TestWriterFlow_NeverRetypesMarkers_Integration is the engine-side
// integration half of the Cx-SCAFFOLD-FRAGMENT-FRAMES RED→GREEN bundle.
// Scenario: the scaffolded README is written to the mount; a writer
// Edits the REPLACE-THIS-LINE placeholder between each marker pair
// with real content; `isValidAppREADME` accepts the result and
// `OverlayRealREADMEs` promotes it into the deliverable. The
// unedited scaffold — with the REPLACE-THIS-LINE placeholders still
// in place — must NOT be accepted (else we would publish a README
// with placeholder comments to zerops.io/recipes).
func TestWriterFlow_NeverRetypesMarkers_Integration(t *testing.T) {
	dir := t.TempDir()
	appdevDir := filepath.Join(dir, "appdev")
	if err := os.MkdirAll(appdevDir, 0o755); err != nil {
		t.Fatal(err)
	}

	plan := &RecipePlan{
		Framework: "bun",
		Slug:      "bun-minimal",
		Targets:   []RecipeTarget{{Hostname: "app", Type: "bun@1.2"}},
	}
	scaffold := GenerateAppREADME(plan)

	// Scaffold written as-is: isValidAppREADME must reject so the
	// deliverable never publishes REPLACE-THIS-LINE placeholders.
	if isValidAppREADME(scaffold) {
		t.Error("unedited scaffold (REPLACE-THIS-LINE placeholders) must be rejected by isValidAppREADME")
	}

	// Simulate a writer Edit: the three REPLACE-THIS-LINE comments get
	// swapped for real content. The markers never move.
	edited := strings.Replace(
		scaffold,
		placeholderLine("intro"),
		"A Bun runtime demo with a PostgreSQL connection.",
		1,
	)
	edited = strings.Replace(
		edited,
		placeholderLine("integration-guide"),
		"### 1. Adding `zerops.yaml`\n\n```yaml\nzerops: []\n```",
		1,
	)
	edited = strings.Replace(
		edited,
		placeholderLine("knowledge-base"),
		"### Gotchas\n\n- **500 on startup** — missing env var.",
		1,
	)

	if !isValidAppREADME(edited) {
		t.Errorf("writer-edited README must be accepted by isValidAppREADME; got:\n%s", edited)
	}

	if err := os.WriteFile(filepath.Join(appdevDir, "README.md"), []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}

	files := map[string]string{"appdev/README.md": scaffold}
	oldBase := recipeMountBaseOverride
	recipeMountBaseOverride = dir
	defer func() { recipeMountBaseOverride = oldBase }()

	if got := OverlayRealREADMEs(files, plan); got != 1 {
		t.Fatalf("expected 1 overlay, got %d", got)
	}
	if files["appdev/README.md"] != edited {
		t.Errorf("overlay did not promote writer-edited README:\n%s", files["appdev/README.md"])
	}

	// Every original marker line survives the Edit.
	for _, marker := range []string{
		"<!-- #ZEROPS_EXTRACT_START:intro# -->",
		"<!-- #ZEROPS_EXTRACT_END:intro# -->",
		"<!-- #ZEROPS_EXTRACT_START:integration-guide# -->",
		"<!-- #ZEROPS_EXTRACT_END:integration-guide# -->",
		"<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->",
		"<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->",
	} {
		if !strings.Contains(files["appdev/README.md"], marker) {
			t.Errorf("marker missing after Edit-between-markers flow: %q", marker)
		}
	}
}

// placeholderLine returns the exact REPLACE-THIS-LINE comment the
// scaffold emits between the markers for the named fragment. Tests
// treat it as load-bearing text because `strings.Replace` must find
// it character-for-character.
func placeholderLine(fragment string) string {
	switch fragment {
	case "intro":
		return "<!-- REPLACE THIS LINE with a 1-3 line plain-prose intro naming the runtime + the managed services. No H2/H3 inside the markers. -->"
	case "integration-guide":
		return "<!-- REPLACE THIS LINE with 3-6 H3 items (\"### 1. Adding `zerops.yaml`\", \"### 2. ...\"), each with a fenced code block. -->"
	case "knowledge-base":
		return "<!-- REPLACE THIS LINE with \"### Gotchas\" followed by 3-6 bullets in `**symptom** -- mechanism` form. -->"
	}
	return ""
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
