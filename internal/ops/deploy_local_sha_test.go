// Tests for: ops/deploy_local.go — the sha parameter (docs/spec-workflows.md
// §4.9): DeployLocal resolves an explicit sha, extracts its tree into a temp
// dir outside workingDir, and pushes from there with --version-name.
package ops

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// initGitRepoWithZerops creates a real git repo at a temp dir with one
// commit containing zerops.yaml, returning (dir, full commit sha). Real
// git commands run for real here (ops/git.LocalRunner uses os/exec
// directly, bypassing the ops.runner test double entirely) — that's the
// same real git binary any dev machine or eval container has.
func initGitRepoWithZerops(t *testing.T) (dir, sha string) {
	t.Helper()
	dir = t.TempDir()
	run := func(args ...string) {
		cmd := exec.CommandContext(context.Background(), "git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=zcp-test", "GIT_AUTHOR_EMAIL=zcp-test@example.com",
			"GIT_COMMITTER_NAME=zcp-test", "GIT_COMMITTER_EMAIL=zcp-test@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "zerops.yaml"), []byte("zerops:\n  - setup: app\n"), 0o644); err != nil {
		t.Fatalf("write zerops.yaml: %v", err)
	}
	run("init", "-q", "-b", "main")
	run("add", "zerops.yaml")
	run("commit", "-q", "-m", "initial")
	out, err := exec.CommandContext(context.Background(), "git", "-C", dir, "rev-parse", "HEAD").CombinedOutput()
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v\n%s", err, out)
	}
	return dir, strings.TrimSpace(string(out))
}

func TestDeployLocal_WithSHA_ResolvesExtractsAndPassesVersionName(t *testing.T) {
	if testing.Short() {
		t.Skip("shells out to the real git binary")
	}
	dir, sha := initGitRepoWithZerops(t)

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "appstage", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
		})
	var extractedDir string
	var zeropsYamlPresentDuringPush bool
	mr := &mockRunner{runResults: []runResult{{}, {}}}
	mr.onRun = func(name string, args []string) {
		if name == "zcli" && len(args) > 0 && args[0] == "push" {
			for i, a := range args {
				if a == "--working-dir" && i+1 < len(args) {
					extractedDir = args[i+1]
					if _, statErr := os.Stat(filepath.Join(extractedDir, "zerops.yaml")); statErr == nil {
						zeropsYamlPresentDuringPush = true
					}
				}
			}
		}
	}
	restore := OverrideRunnerForTest(mr)
	defer restore()

	result, err := DeployLocal(context.Background(), mock, "proj-1", localTestAuth(),
		"appstage", "", dir, sha[:7])
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.SHA != sha {
		t.Errorf("result.SHA = %q, want %q", result.SHA, sha)
	}

	pushArgs := strings.Join(mr.runCalls[1].args, " ")
	if !strings.Contains(pushArgs, "--version-name "+sha) {
		t.Errorf("push args should contain --version-name %s, got: %s", sha, pushArgs)
	}
	if !strings.Contains(pushArgs, "--no-git") {
		t.Errorf("push args should still contain --no-git, got: %s", pushArgs)
	}
	if extractedDir == "" {
		t.Fatal("push should carry --working-dir pointing at the extracted tree")
	}
	if strings.HasPrefix(extractedDir, dir) {
		t.Errorf("extracted dir %q must be outside the repo dir %q", extractedDir, dir)
	}
	if !zeropsYamlPresentDuringPush {
		t.Error("extracted dir should have contained zerops.yaml at push time")
	}
	// Cleanup: the tmp dir must be removed after the push (best-effort,
	// checked here since DeployLocal defers the removal before returning).
	if _, statErr := os.Stat(extractedDir); statErr == nil {
		t.Errorf("extracted tmp dir %q should have been removed after the push", extractedDir)
	}
}

func TestDeployLocal_WithUnresolvableSHA_ReturnsInvalidParameterError(t *testing.T) {
	if testing.Short() {
		t.Skip("shells out to the real git binary")
	}
	dir, _ := initGitRepoWithZerops(t)

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "appstage", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
		})
	mr := &mockRunner{}
	restore := OverrideRunnerForTest(mr)
	defer restore()

	_, err := DeployLocal(context.Background(), mock, "proj-1", localTestAuth(),
		"appstage", "", dir, "deadbeefdeadbeef")
	if err == nil {
		t.Fatal("expected error for an unresolvable sha")
	}
	var pe *platform.PlatformError
	if !errorAs(err, &pe) {
		t.Fatalf("expected PlatformError, got %T: %v", err, err)
	}
	if pe.Code != platform.ErrInvalidParameter {
		t.Errorf("code = %s, want %s", pe.Code, platform.ErrInvalidParameter)
	}
	if len(mr.runCalls) != 0 {
		t.Errorf("no zcli command should run when sha does not resolve, got %d calls", len(mr.runCalls))
	}
}

// TestDeployLocal_NoSHA_SourceHasCleanRepo_RecordsHEADAndKeepsPushArgsUnchanged
// pins item 4 (docs/spec-workflows.md §4.9): a working-tree deploy (no
// explicit sha) whose workingDir is a git repo with a reachable HEAD
// records that HEAD as SHA, Dirty=false — and the zcli push args (no
// --version-name) stay exactly what a no-git-repo source would have
// produced.
func TestDeployLocal_NoSHA_SourceHasCleanRepo_RecordsHEADAndKeepsPushArgsUnchanged(t *testing.T) {
	if testing.Short() {
		t.Skip("shells out to the real git binary")
	}
	dir, sha := initGitRepoWithZerops(t)

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "appstage", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
		})
	mr := &mockRunner{runResults: []runResult{{}, {}}}
	restore := OverrideRunnerForTest(mr)
	defer restore()

	result, err := DeployLocal(context.Background(), mock, "proj-1", localTestAuth(),
		"appstage", "", dir, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.SHA != sha {
		t.Errorf("result.SHA = %q, want %q (source repo's HEAD)", result.SHA, sha)
	}
	if result.Dirty {
		t.Error("result.Dirty = true, want false (clean status)")
	}
	if len(mr.runCalls) != 2 {
		t.Fatalf("zcli calls = %d, want 2 (login, push)", len(mr.runCalls))
	}
	pushArgs := strings.Join(mr.runCalls[1].args, " ")
	if strings.Contains(pushArgs, "--version-name") {
		t.Errorf("push args must NOT contain --version-name for a working-tree deploy, got: %s", pushArgs)
	}
	if !strings.Contains(pushArgs, "--working-dir "+dir) {
		t.Errorf("push args should carry --working-dir %s (the repo itself, no extraction), got: %s", dir, pushArgs)
	}
}

// TestDeployLocal_NoSHA_SourceHasDirtyRepo_RecordsDirty pins the
// Dirty=true case for local mode.
func TestDeployLocal_NoSHA_SourceHasDirtyRepo_RecordsDirty(t *testing.T) {
	if testing.Short() {
		t.Skip("shells out to the real git binary")
	}
	dir, sha := initGitRepoWithZerops(t)
	if err := os.WriteFile(filepath.Join(dir, "zerops.yaml"), []byte("zerops:\n  - setup: app\n  - setup: app2\n"), 0o644); err != nil {
		t.Fatalf("dirty the tree: %v", err)
	}

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "appstage", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22"}},
		})
	mr := &mockRunner{runResults: []runResult{{}, {}}}
	restore := OverrideRunnerForTest(mr)
	defer restore()

	result, err := DeployLocal(context.Background(), mock, "proj-1", localTestAuth(),
		"appstage", "", dir, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.SHA != sha {
		t.Errorf("result.SHA = %q, want %q (HEAD unchanged by an uncommitted edit)", result.SHA, sha)
	}
	if !result.Dirty {
		t.Error("result.Dirty = false, want true")
	}
}
