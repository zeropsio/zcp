//go:build e2e

package e2e_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestLocalGitFixture_InheritedRepositoryBindings_PreservesCheckoutConfig(t *testing.T) {
	// non-parallel: simulate the Git environment inherited from a hook.
	checkout := t.TempDir()
	cmd := exec.CommandContext(t.Context(), "git", "init", "-q")
	cmd.Dir = checkout
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + t.TempDir(), "GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_CONFIG_NOSYSTEM=1"}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("init enclosing checkout: %v\n%s", err, out)
	}
	config := filepath.Join(checkout, ".git", "config")
	before, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_DIR", filepath.Join(checkout, ".git"))
	t.Setenv("GIT_WORK_TREE", checkout)
	t.Setenv("GIT_INDEX_FILE", filepath.Join(checkout, ".git", "index"))
	fixture := createBrokenBuildApp(t, "fixture")
	after, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Error("local Git fixture changed enclosing checkout config")
	}
	cmd = exec.CommandContext(t.Context(), "git", "--git-dir="+filepath.Join(fixture, ".git"), "log", "-1", "--pretty=%s")
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_CONFIG_NOSYSTEM=1"}
	out, err := cmd.CombinedOutput()
	if err != nil || string(out) != "init\n" {
		t.Fatalf("fixture's own initial commit = %q, %v; want init", out, err)
	}
}
