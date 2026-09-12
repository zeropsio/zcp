package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEvalFarmCoverage_Verb_WritesMarkdownNextToBatch pins the tool layer:
// `zcp eval farm coverage <dir>` (ZCP_AUTHORING=1) prints the deterministic
// table to stdout and writes the identical body to <dir>/coverage.md.
func TestEvalFarmCoverage_Verb_WritesMarkdownNextToBatch(t *testing.T) {
	t.Setenv("ZCP_AUTHORING", "1")

	repoRoot, err := filepath.Abs("../../internal/eval/farm/testdata/coverage/basic")
	if err != nil {
		t.Fatalf("Abs: %v", err)
	}
	dir := t.TempDir()
	if err := copyTree(t, repoRoot, dir); err != nil {
		t.Fatalf("copyTree: %v", err)
	}

	var code int
	stdout, stderr := captureOutput(t, func() {
		code = runEvalFarm([]string{"coverage", dir})
	})
	if code != 0 {
		t.Fatalf("runEvalFarm(coverage) = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "zerops_deploy") {
		t.Errorf("stdout = %q, want it to contain zerops_deploy", stdout)
	}

	written, err := os.ReadFile(filepath.Join(dir, "coverage.md"))
	if err != nil {
		t.Fatalf("ReadFile coverage.md: %v", err)
	}
	if string(written) != stdout {
		t.Errorf("coverage.md = %q, want it to equal stdout %q", written, stdout)
	}
}

func copyTree(t *testing.T, src, dst string) error {
	t.Helper()
	return filepath.WalkDir(src, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, body, 0o644)
	})
}
