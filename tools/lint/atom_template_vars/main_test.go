package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/analyze"
)

// TestAtomTemplateVarsLint_ProductionAtomsClean asserts the live atom
// corpus is lint-clean. A failure here means someone edited an atom
// to reference an un-populated template field — the v36 F-9 regression
// class. Ship a Cx-ENVFOLDERS-WIRED companion commit before adding
// new fields.
//
// This test runs `go test ./tools/lint/atom_template_vars/...` via CI
// alongside the `go run` invocation in the Makefile, so the lint is
// double-guarded at the CI layer + the local-dev-machine layer.
func TestAtomTemplateVarsLint_ProductionAtomsClean(t *testing.T) {
	t.Parallel()
	root := findRepoRoot(t)
	result := analyze.CheckAtomTemplateVarsBound(filepath.Join(root, atomRoot), analyze.DefaultAllowedAtomFields)
	if result.Status == analyze.StatusSkip {
		t.Fatalf("skip: %s", result.Reason)
	}
	if result.Status != analyze.StatusPass {
		t.Fatalf("%d unbound atom template references:\n  %s",
			result.Observed, strings.Join(result.EvidenceFiles, "\n  "))
	}
}

// TestAtomTemplateVarsLint_FixtureFailsOnUnbound pins the opposite
// direction: a fixture tree with an unbound `{{.FakeField}}` must
// produce a fail result. This prevents a silent regression where the
// regex stops matching (e.g., someone broadens DefaultAllowedAtomFields
// to include every capitalized word).
func TestAtomTemplateVarsLint_FixtureFailsOnUnbound(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bad.md"), []byte("Header: {{.FakeField}}\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	allowed := map[string]bool{"ProjectRoot": true}
	got := analyze.CheckAtomTemplateVarsBound(dir, allowed)
	if got.Status != analyze.StatusFail {
		t.Fatalf("fixture with .FakeField should fail: got %s", got.Status)
	}
	if got.Observed != 1 {
		t.Errorf("observed=%d want=1", got.Observed)
	}
}

// findRepoRoot walks up from the test package dir to the module root
// (first ancestor containing go.mod). Mirrors the helper in
// tools/lint/recipe_atom_lint_test.go.
func findRepoRoot(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	dir := cwd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod ancestor of %s", cwd)
		}
		dir = parent
	}
}
