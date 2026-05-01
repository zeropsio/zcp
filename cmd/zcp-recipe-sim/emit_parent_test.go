// Tests for S4 — Parent + MountRoot threading through emit. Without
// these, the codebase-content brief's parent_recipe_dedup logic is
// unverified in sim because the brief composer never sees a populated
// `*ParentRecipe`. Spec: docs/zcprecipator3/plans/run-20-prep.md §S4.
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRunEmit_PassesParentToBriefComposer asserts that, given a
// CLI-supplied `-parent <slug>` and a populated mount-root containing
// the parent recipe's tree, the emitted codebase-content prompt
// references the parent recipe surfaces. The codebase-content brief
// emits a "## Parent recipe `<slug>`" pointer at briefs_content_phase.go:130
// when parent != nil. Pinned by run-20 prep S4.
func TestRunEmit_PassesParentToBriefComposer(t *testing.T) {
	runDir := t.TempDir()
	simDir := t.TempDir()
	mountRoot := t.TempDir()

	if err := writeMinimalRunDir(t, runDir); err != nil {
		t.Fatalf("writeMinimalRunDir: %v", err)
	}
	// Parent slug = "<framework>-minimal". writeMinimalRunDir's plan
	// uses slug=fixture-recipe; the chain resolver derives parent
	// from `-showcase` suffix only. Use a slug that triggers chain.
	rewriteSlug(t, filepath.Join(runDir, "environments", "plan.json"), "fixture-showcase")
	parentSlug := "fixture-minimal"
	parentDir := filepath.Join(mountRoot, parentSlug)
	mustWrite(t, filepath.Join(parentDir, "import.yaml"), "project:\n  name: fixture\n")
	mustWrite(t, filepath.Join(parentDir, "codebases", "api", "README.md"),
		"# Parent api codebase README\n")
	mustWrite(t, filepath.Join(parentDir, "codebases", "api", "zerops.yaml"),
		"#bare\nzerops:\n")

	if err := runEmit([]string{
		"-run", runDir,
		"-out", simDir,
		"-mount-root", mountRoot,
		"-parent", parentSlug,
	}); err != nil {
		t.Fatalf("runEmit: %v", err)
	}

	body, err := os.ReadFile(filepath.Join(simDir, "briefs", "api-prompt.md"))
	if err != nil {
		t.Fatalf("read api-prompt.md: %v", err)
	}
	bodyStr := string(body)
	// The codebase-content brief composer's pointer-block emits the
	// parent recipe pointer when parent != nil. Asserting on the slug
	// reference is sufficient — the exact phrasing belongs to the
	// composer (sub-agent B's territory).
	if !strings.Contains(bodyStr, parentSlug) {
		t.Errorf("emitted prompt does not reference parent slug %q; brief composer not seeing populated *ParentRecipe", parentSlug)
	}
}

// TestRunEmit_NoParentFlag_OmitsParentPointer asserts the legacy
// behavior remains: without -parent, no parent recipe pointer in the
// emitted prompt. Pinned by run-20 prep S4 (negative case).
func TestRunEmit_NoParentFlag_OmitsParentPointer(t *testing.T) {
	runDir := t.TempDir()
	simDir := t.TempDir()
	if err := writeMinimalRunDir(t, runDir); err != nil {
		t.Fatalf("writeMinimalRunDir: %v", err)
	}

	if err := runEmit([]string{"-run", runDir, "-out", simDir}); err != nil {
		t.Fatalf("runEmit: %v", err)
	}

	body, err := os.ReadFile(filepath.Join(simDir, "briefs", "api-prompt.md"))
	if err != nil {
		t.Fatalf("read api-prompt.md: %v", err)
	}
	bodyStr := string(body)
	if strings.Contains(bodyStr, "Parent recipe") {
		t.Errorf("emitted prompt without -parent must omit parent pointer; got body containing 'Parent recipe'")
	}
}

// TestRunEmit_ParentFlagWithoutMountRoot_Errors asserts the CLI
// rejects `-parent` without `-mount-root` (parent can't be loaded).
// Pinned by run-20 prep S4.
func TestRunEmit_ParentFlagWithoutMountRoot_Errors(t *testing.T) {
	runDir := t.TempDir()
	simDir := t.TempDir()
	if err := writeMinimalRunDir(t, runDir); err != nil {
		t.Fatalf("writeMinimalRunDir: %v", err)
	}

	err := runEmit([]string{
		"-run", runDir, "-out", simDir,
		"-parent", "some-parent",
	})
	if err == nil {
		t.Fatalf("expected error: -parent without -mount-root")
	}
	if !strings.Contains(err.Error(), "mount-root") {
		t.Errorf("error %q does not mention mount-root", err.Error())
	}
}

// rewriteSlug is a small test-only helper that loads a plan.json,
// patches the `slug` field, and writes back. Avoids re-marshalling
// the whole Plan struct just for the test fixture's chain trigger.
func rewriteSlug(t *testing.T, path, newSlug string) {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	in := string(body)
	const prefix = "\"slug\": \""
	i := strings.Index(in, prefix)
	if i < 0 {
		t.Fatalf("plan.json has no slug field: %s", path)
	}
	j := strings.IndexByte(in[i+len(prefix):], '"')
	if j < 0 {
		t.Fatalf("plan.json malformed slug: %s", path)
	}
	out := in[:i+len(prefix)] + newSlug + in[i+len(prefix)+j:]
	if err := os.WriteFile(path, []byte(out), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
