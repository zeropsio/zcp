package farm

import (
	"os"
	"path/filepath"
	"testing"
)

// gatesetRepoRoot walks up from the test binary's working directory until it
// finds `go.mod` — the repo root. Shared with controller_test.go.
func gatesetRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repo root (go.mod)")
		}
		dir = parent
	}
}
