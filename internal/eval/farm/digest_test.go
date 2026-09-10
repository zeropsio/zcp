package farm

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDigest_TreeDigest_StableAcrossOrderAndPlatform pins the tree-digest
// rule (docs/spec-eval-farm.md §1.1): sha256 over the sorted list of
// "relative-path\n" + file-sha256 lines. Writing the same files in a
// different creation order must produce the same digest; renaming one file
// must change it.
func TestDigest_TreeDigest_StableAcrossOrderAndPlatform(t *testing.T) {
	t.Parallel()

	writeTree := func(t *testing.T, order []string) string {
		t.Helper()
		dir := t.TempDir()
		content := map[string]string{
			"a.txt":        "alpha",
			"sub/b.txt":    "beta",
			"sub/c/d.json": `{"k":"v"}`,
		}
		for _, name := range order {
			path := filepath.Join(dir, name)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatalf("MkdirAll: %v", err)
			}
			if err := os.WriteFile(path, []byte(content[name]), 0o644); err != nil {
				t.Fatalf("WriteFile: %v", err)
			}
		}
		return dir
	}

	dirA := writeTree(t, []string{"a.txt", "sub/b.txt", "sub/c/d.json"})
	dirB := writeTree(t, []string{"sub/c/d.json", "a.txt", "sub/b.txt"})

	digestA, err := TreeDigest(dirA)
	if err != nil {
		t.Fatalf("TreeDigest(dirA): %v", err)
	}
	digestB, err := TreeDigest(dirB)
	if err != nil {
		t.Fatalf("TreeDigest(dirB): %v", err)
	}
	if digestA != digestB {
		t.Errorf("TreeDigest differs by creation order: %s != %s", digestA, digestB)
	}

	// A renamed file must change the digest.
	dirC := t.TempDir()
	if err := os.WriteFile(filepath.Join(dirC, "a.txt"), []byte("alpha"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dirC, "sub", "c"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dirC, "sub", "b-renamed.txt"), []byte("beta"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dirC, "sub", "c", "d.json"), []byte(`{"k":"v"}`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	digestC, err := TreeDigest(dirC)
	if err != nil {
		t.Fatalf("TreeDigest(dirC): %v", err)
	}
	if digestC == digestA {
		t.Errorf("TreeDigest(renamed) = %s, want it to differ from %s", digestC, digestA)
	}
}
