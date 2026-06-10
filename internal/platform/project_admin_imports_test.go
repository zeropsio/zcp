package platform_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestProjectAdminClientRestrictedImport pins P-LP-2: the cross-project
// admin surface is reachable ONLY from the launch-production trust
// boundary. Two seams are checked:
//
//  1. Direct symbol references — platform.NewProjectAdminClient /
//     platform.ProjectAdminClient. Allowed: workflow_launch_production.go
//     (handler entrypoint + factory definition) and launch_pipeline.go
//     (Part 2 sibling: pipeline-config check).
//  2. Factory-var laundering — the package vars projectAdminClientFactory
//     and existingProdTokenClientFactory hand out an authenticated
//     cross-project client WITHOUT the platform.* literal, so a grep on
//     symbols alone misses callers (launch_reset.go reached the admin
//     client this way while the old test pinned only seam 1). Allowed
//     callers: the trust-boundary files that already own a per-call
//     credential input (launchKey / existingProdToken).
//
// This is a structural grep test, NOT a method-call-graph analyzer —
// the goal is unambiguous failure when bleed happens. Strong signal,
// trivial to maintain.
func TestProjectAdminClientRestrictedImport(t *testing.T) {
	t.Parallel()

	// Locate workspace root by walking up to find go.mod.
	root := findWorkspaceRoot(t)

	// Seam 1: direct platform.* symbol references.
	allowedSymbolFiles := map[string]bool{
		"workflow_launch_production.go": true,
		"launch_pipeline.go":            true,
	}
	// Seam 2: factory-var references (definition, setter, or call).
	allowedFactoryFiles := map[string]bool{
		"workflow_launch_production.go": true, // projectAdminClientFactory definition + mutation call
		"launch_pipeline.go":            true, // pipeline resume re-check
		"launch_reset.go":               true, // launchKey-bearing orphan-project delete
		"launch_existing.go":            true, // existingProdTokenClientFactory definition + existing-project path
	}

	toolsDir := filepath.Join(root, "internal", "tools")

	var violations []string
	err := filepath.WalkDir(toolsDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		// Skip test files — they may legitimately reference the type for mocking.
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		content := string(data)
		base := filepath.Base(path)

		referencesAdmin :=
			strings.Contains(content, "platform.NewProjectAdminClient") ||
				strings.Contains(content, "platform.ProjectAdminClient")
		if referencesAdmin && !allowedSymbolFiles[base] {
			violations = append(violations, path+" (direct ProjectAdminClient symbol)")
		}

		referencesFactory :=
			strings.Contains(content, "projectAdminClientFactory") ||
				strings.Contains(content, "existingProdTokenClientFactory")
		if referencesFactory && !allowedFactoryFiles[base] {
			violations = append(violations, path+" (cross-project client factory reference)")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk tools dir: %v", err)
	}

	if len(violations) > 0 {
		t.Fatalf("P-LP-2 violation: cross-project admin surface leaked outside the launch trust boundary:\n%s",
			strings.Join(violations, "\n"))
	}
}

func findWorkspaceRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for range 10 {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not find go.mod from %s", dir)
		}
		dir = parent
	}
	t.Fatalf("walked too far up looking for go.mod")
	return ""
}
