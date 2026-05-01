package recipe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestStitchContent_DoesNotWipeCodebaseFiles — run-23 fix-2
// reproducer attempt for the run-20 "appdev/workerdev wiped to 0
// bytes" bug. apidev had complete fragments, appdev/workerdev had
// sparse fragments; in production the latter two ended up with empty
// zerops.yaml + README files. apidev was unaffected.
//
// Hypothesis test: stage three codebases on disk with bare scaffold
// yaml + README placeholder, record COMPLETE fragments only for the
// first codebase, leave the other two with sparse fragments, run
// stitchContent and assert no surface ends up at 0 bytes for any
// codebase.
//
// If this test FAILS we have local repro of the bug — the failing
// path is the wipe vector. If it PASSES the bug is in a path not
// reachable through stitchContent (e.g. an intermediate handler
// invocation, or environment-specific filesystem behavior).
func TestStitchContent_DoesNotWipeCodebaseFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	outputRoot := filepath.Join(dir, "run")
	store := NewStore(dir)
	dispatch(t.Context(), store, RecipeInput{
		Action: "start", Slug: "synth-showcase", OutputRoot: outputRoot,
	})
	sess, _ := store.Get("synth-showcase")
	sess.Plan = syntheticShowcasePlan()
	stageScaffoldYAMLs(t, dir, sess.Plan)

	// Record fragments — apidev gets a complete set; appdev gets
	// only the intro; workerdev gets nothing.
	completeFragments := []recordFragmentCall{
		{ID: "root/intro", Body: "synth showcase intro"},
		{ID: "env/0/intro", Body: "AI Agent tier"},
		{ID: "env/1/intro", Body: "Remote CDE tier"},
		{ID: "env/2/intro", Body: "Local tier"},
		{ID: "env/3/intro", Body: "Stage tier"},
		{ID: "env/4/intro", Body: "Small Prod tier"},
		{ID: "env/5/intro", Body: "HA Prod tier"},
		// api: full set
		{ID: "codebase/api/intro", Body: "api intro"},
		{ID: "codebase/api/integration-guide", Body: "1. Bind to 0.0.0.0", Class: "platform-invariant"},
		{ID: "codebase/api/knowledge-base", Body: "- **404 on x** — because Y", Class: "platform-invariant"},
		{ID: "codebase/api/claude-md", Body: initStyleClaudeMD("api")},
		// app: intro only — IG/KB/CLAUDE missing
		{ID: "codebase/app/intro", Body: "app intro"},
		// worker: nothing recorded
	}
	for _, f := range completeFragments {
		res := dispatch(t.Context(), store, RecipeInput{
			Action: "record-fragment", Slug: "synth-showcase",
			FragmentID: f.ID, Fragment: f.Body, Classification: f.Class,
		})
		if !res.OK {
			t.Fatalf("record-fragment %s: %+v", f.ID, res)
		}
	}

	// Run stitch — sparse fragments will surface as missing, but
	// surfaces still get written.
	dispatch(t.Context(), store, RecipeInput{
		Action: "stitch-content", Slug: "synth-showcase",
	})

	// For every codebase, assert the on-disk surfaces ARE NOT empty.
	// This is the run-20 wipe-vector check: the bug produced 0-byte
	// files; non-empty ≠ "good content" but it ≠ "wiped" either.
	for _, cb := range sess.Plan.Codebases {
		surfaces := []string{
			filepath.Join(cb.SourceRoot, "zerops.yaml"),
			filepath.Join(cb.SourceRoot, "README.md"),
			filepath.Join(cb.SourceRoot, "CLAUDE.md"),
		}
		for _, p := range surfaces {
			info, err := os.Stat(p)
			if err != nil {
				t.Errorf("codebase %s: stat %s: %v", cb.Hostname, p, err)
				continue
			}
			if info.Size() == 0 {
				t.Errorf("WIPE REPRO — codebase %s: %s is 0 bytes", cb.Hostname, p)
			}
		}
	}
}

// TestStitchYAML_RefusesEmptyWriteOverNonEmpty — run-23 fix-2 guard.
// Even if the upstream pipeline produces an empty body for some
// reason (missing-fragment edge case, all-comment yaml stripped to
// nothing, classifier returning empty), we MUST refuse to overwrite
// a non-empty on-disk file with 0 bytes. The wipe vector in run-20
// resulted in 0-byte zerops.yaml; this guard makes it impossible.
func TestStitchYAML_RefusesEmptyWriteOverNonEmpty(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	srcRoot := filepath.Join(dir, "appdev")
	if err := os.MkdirAll(srcRoot, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	yamlPath := filepath.Join(srcRoot, "zerops.yaml")
	original := "zerops:\n  - setup: appdev\n    run:\n      base: nodejs@22\n      start: zsc noop --silent\n"
	if err := os.WriteFile(yamlPath, []byte(original), 0o600); err != nil {
		t.Fatalf("write yaml: %v", err)
	}

	// Plan with a codebase whose fragments DELIBERATELY transform the
	// stripped yaml to empty — exercise the wipe path.
	plan := &Plan{
		Slug: "wipe-test",
		Codebases: []Codebase{
			{Hostname: "app", SourceRoot: srcRoot},
		},
	}

	if err := WriteCodebaseYAMLWithComments(plan, "app"); err != nil {
		t.Fatalf("WriteCodebaseYAMLWithComments: %v", err)
	}

	// File must still be non-empty; original yaml content survives.
	info, err := os.Stat(yamlPath)
	if err != nil {
		t.Fatalf("stat yaml: %v", err)
	}
	if info.Size() == 0 {
		t.Fatalf("WIPE — yaml was overwritten with 0 bytes despite original being non-empty")
	}
	body, _ := os.ReadFile(yamlPath)
	if !strings.Contains(string(body), "zerops:") {
		t.Errorf("yaml body should still contain `zerops:` directive; got %q", body)
	}
}
