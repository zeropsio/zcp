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

// TestStitchYAML_RefusesEmptyWriteOverNonEmpty — run-23 fix-2 guard
// kept under run-21-prep whole-yaml shape. If the agent records an
// empty/whitespace-only body for `codebase/<h>/zerops-yaml` (broken
// edit, accidental wipe, copy-paste failure), the stitcher MUST refuse
// rather than overwrite the bare scaffold yaml with 0 bytes. The wipe
// vector in run-20 resulted in 0-byte zerops.yaml on appdev/workerdev;
// this guard closes it by construction.
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

	// Whole-yaml fragment recorded with whitespace-only body — exercises
	// the wipe path. Pre-fix the test recorded NO fragment, hit the
	// "fragment absent" early-return, and never validated the guard.
	plan := &Plan{
		Slug: "wipe-test",
		Codebases: []Codebase{
			{Hostname: "app", SourceRoot: srcRoot},
		},
		Fragments: map[string]string{
			"codebase/app/zerops-yaml": "   \n  \n",
		},
	}

	err := WriteCodebaseYAMLWithComments(plan, "app")
	if err == nil {
		t.Fatalf("WriteCodebaseYAMLWithComments should refuse empty fragment over non-empty disk")
	}
	if !strings.Contains(err.Error(), "refuse-to-wipe") {
		t.Errorf("error should name refuse-to-wipe; got %v", err)
	}

	// File must still be non-empty; original yaml content survives.
	body, readErr := os.ReadFile(yamlPath)
	if readErr != nil {
		t.Fatalf("stat yaml: %v", readErr)
	}
	if string(body) != original {
		t.Errorf("on-disk yaml mutated despite refusal\n--- got\n%s\n--- want\n%s", body, original)
	}
}
