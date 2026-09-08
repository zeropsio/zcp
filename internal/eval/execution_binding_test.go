package eval

import (
	"os"
	"path/filepath"
	"testing"
)

// TestExecutionBinding_UnsafeOrOverlappingPaths_Refused pins
// docs/spec-testing-architecture.md §10.4 "Preflight": work dir and results
// dir must be absolute, distinct, not nested in each other, not "/", and
// not a home directory root.
func TestExecutionBinding_UnsafeOrOverlappingPaths_Refused(t *testing.T) {
	t.Parallel()
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("no resolvable home directory in this environment")
	}
	root := t.TempDir()
	work := filepath.Join(root, "work")
	results := filepath.Join(root, "results")

	cases := []struct {
		name    string
		workDir string
		results string
	}{
		{"nested: results inside work", work, filepath.Join(work, "results")},
		{"nested: work inside results", filepath.Join(results, "work"), results},
		{"equal", work, work},
		{"relative work dir", "relative/work", results},
		{"relative results dir", work, "relative/results"},
		{"work dir is root", "/", results},
		{"results dir is root", work, "/"},
		{"work dir is home", home, results},
		{"results dir is home", work, home},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := assertSafeRoots(tc.workDir, tc.results); err == nil {
				t.Fatalf("assertSafeRoots(%q, %q) = nil, want an error", tc.workDir, tc.results)
			}
		})
	}

	if err := assertSafeRoots(work, results); err != nil {
		t.Fatalf("assertSafeRoots(%q, %q) = %v, want nil for two distinct absolute non-nested dirs", work, results, err)
	}
}
