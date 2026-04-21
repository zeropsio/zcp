package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRunDryRunRecipe_ShowcaseAgainstGoldens runs the dry-run harness
// against the live step-4 goldens at
// docs/zcprecipator2/04-verification/ and asserts no diff. This is the
// pre-v35 ship gate per rollout-sequence.md §C-14: `zcp dry-run recipe
// --tier=showcase` must pass against goldens before the v35 showcase
// run is commissioned.
func TestRunDryRunRecipe_ShowcaseAgainstGoldens(t *testing.T) {
	t.Parallel()
	root := findRepoRoot(t)
	goldenDir := filepath.Join(root, "docs", "zcprecipator2", "04-verification")
	if _, err := os.Stat(goldenDir); err != nil {
		t.Skipf("golden directory missing: %v", err)
	}

	var stdout, stderr bytes.Buffer
	exit := runDryRunRecipe(
		[]string{"--tier=showcase", "--against=" + goldenDir},
		&stdout, &stderr,
	)
	if exit != 0 {
		t.Fatalf("dry-run recipe exit=%d; stdout=\n%s\nstderr=\n%s", exit, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "dry-run recipe: ok") {
		t.Errorf("stdout must end with ok sentinel; got:\n%s", stdout.String())
	}
	// Every §C-14 showcase brief must compose to non-empty bytes.
	for _, role := range []string{"code-review", "editorial-review", "feature", "writer"} {
		if !strings.Contains(stdout.String(), role+":") {
			t.Errorf("stdout missing %s brief; got:\n%s", role, stdout.String())
		}
	}
}

// TestRunDryRunRecipe_MinimalShape confirms the minimal-tier synthesis
// produces a smaller target list + the single-runtime minimal path
// composes one scaffold brief (app), while dual-runtime minimal
// composes two (app + api).
func TestRunDryRunRecipe_MinimalShape(t *testing.T) {
	t.Parallel()

	t.Run("single-runtime", func(t *testing.T) {
		t.Parallel()
		var stdout, stderr bytes.Buffer
		exit := runDryRunRecipe([]string{"--tier=minimal"}, &stdout, &stderr)
		if exit != 0 {
			t.Fatalf("exit=%d stderr=%s", exit, stderr.String())
		}
		if !strings.Contains(stdout.String(), "scaffold-app-app:") {
			t.Errorf("single-runtime minimal must compose a scaffold-app brief; got:\n%s", stdout.String())
		}
		if strings.Contains(stdout.String(), "scaffold-api-api:") {
			t.Errorf("single-runtime minimal must NOT compose a scaffold-api brief; got:\n%s", stdout.String())
		}
	})

	t.Run("dual-runtime", func(t *testing.T) {
		t.Parallel()
		var stdout, stderr bytes.Buffer
		exit := runDryRunRecipe([]string{"--tier=minimal", "--dual-runtime"}, &stdout, &stderr)
		if exit != 0 {
			t.Fatalf("exit=%d stderr=%s", exit, stderr.String())
		}
		if !strings.Contains(stdout.String(), "scaffold-app-app:") || !strings.Contains(stdout.String(), "scaffold-api-api:") {
			t.Errorf("dual-runtime minimal must compose both scaffold-app + scaffold-api briefs; got:\n%s", stdout.String())
		}
	})
}

// TestRunDryRunRecipe_InvalidTier asserts the harness rejects unknown
// tier values with exit 1 and a useful error on stderr.
func TestRunDryRunRecipe_InvalidTier(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	exit := runDryRunRecipe([]string{"--tier=bogus"}, &stdout, &stderr)
	if exit != 1 {
		t.Fatalf("exit=%d want 1", exit)
	}
	if !strings.Contains(stderr.String(), "unknown tier") {
		t.Errorf("stderr must name unknown tier; got %q", stderr.String())
	}
}

// findRepoRoot walks up from cwd to the nearest directory containing
// go.mod, returning its absolute path. Fails the test when not found
// within 10 levels.
func findRepoRoot(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	dir := cwd
	for range 10 {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatalf("could not find go.mod from %s", cwd)
	return ""
}
