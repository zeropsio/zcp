package platform_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestProjectAdminClientRestrictedImport pins P-LP-2: the constructor
// NewProjectAdminClient is callable ONLY from the launch-production
// workflow surface — workflow_launch_production.go (handler entrypoint)
// and launch_pipeline.go (Part 2 sibling: pipeline-config check). Both
// files are part of the same trust boundary; every other file in
// internal/tools/ that references platform.NewProjectAdminClient or
// platform.ProjectAdminClient is a discipline violation.
//
// This is a structural grep test, NOT a method-call-graph analyzer —
// the goal is unambiguous failure when bleed happens. Strong signal,
// trivial to maintain.
func TestProjectAdminClientRestrictedImport(t *testing.T) {
	t.Parallel()

	// Locate workspace root by walking up to find go.mod.
	root := findWorkspaceRoot(t)

	allowedFiles := map[string]bool{
		"workflow_launch_production.go": true,
		"launch_pipeline.go":            true,
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
		referencesAdmin :=
			strings.Contains(content, "platform.NewProjectAdminClient") ||
				strings.Contains(content, "platform.ProjectAdminClient")
		if !referencesAdmin {
			return nil
		}
		base := filepath.Base(path)
		if allowedFiles[base] {
			return nil
		}
		violations = append(violations, path)
		return nil
	})
	if err != nil {
		t.Fatalf("walk tools dir: %v", err)
	}

	if len(violations) > 0 {
		t.Fatalf("P-LP-2 violation: cross-project ProjectAdminClient symbols leaked outside workflow_launch_production.go:\n%s",
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
