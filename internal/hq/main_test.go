package hq

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/zeropsio/zcp/internal/testgit"
)

// non-parallel: Git environment is isolated once, before package tests start.
func TestMain(m *testing.M) {
	os.Exit(testgit.Run(m))
}

// A hook's repository bindings must not redirect a real fixture's git config
// writes out of its temporary repo and into the checkout enclosing the tests.
func TestPackageGit_InheritedRepositoryBindings_LeavesCheckoutConfigUnchanged(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	cmd := exec.CommandContext(t.Context(), "git", "init", "-q", root)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("init enclosing checkout: %v\n%s", err, out)
	}
	config := filepath.Join(root, ".git", "config")
	before, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	global := filepath.Join(home, "gitconfig")
	const userConfig = "[user]\n\tname = Checkout owner\n\temail = owner@example.invalid\n"
	if err := os.WriteFile(global, []byte(userConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd = exec.CommandContext(t.Context(), executable, "-test.run=^TestGitHubHelperConfig_OnlyZcpAnswersGitHub$", "-test.v")
	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		"GIT_DIR="+filepath.Join(root, ".git"), "GIT_WORK_TREE="+root,
		"GIT_INDEX_FILE="+filepath.Join(root, ".git", "index"),
		"HOME="+home, "GIT_CONFIG_GLOBAL="+global, "GIT_CONFIG_NOSYSTEM=1")
	out, runErr := cmd.CombinedOutput()
	after, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Error("package tests changed enclosing checkout Git config")
	}
	afterGlobal, err := os.ReadFile(global)
	if err != nil {
		t.Fatal(err)
	}
	if string(afterGlobal) != userConfig {
		t.Error("package tests changed the user's Git config")
	}
	if runErr != nil {
		t.Fatalf("real GitHub helper consumer with inherited repository bindings: %v\n%s", runErr, out)
	}
}
