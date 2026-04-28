// Tests for: deploy_local_git.go — handleLocalGitPush. Exercises real
// git binaries against a temp repo with a bare remote, so the user-
// credentials path is still exercised but locally, no network.
package tools

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// gitRepoFixture sets up a temp source repo with one commit, a bare
// "remote" repo alongside, and an origin wired between them. Returns the
// source working dir and the bare remote URL so tests can exercise
// handleLocalGitPush end-to-end without network.
//
// Skips the test when git isn't on PATH (CI image sanity — local macOS
// has git by default; tests that depend on it would otherwise fail with
// a misleading "not a git repo" error).
func gitRepoFixture(t *testing.T) (workDir, remoteURL string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH — skipping real-git test")
	}

	root := t.TempDir()
	workDir = filepath.Join(root, "work")
	remoteDir := filepath.Join(root, "remote.git")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir work: %v", err)
	}

	run := func(dir string, args ...string) {
		//nolint:gosec // test-only, inputs are t.TempDir paths
		cmd := exec.CommandContext(context.Background(), "git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=t@t.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=t@t.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	// Bare remote.
	if err := os.MkdirAll(remoteDir, 0o755); err != nil {
		t.Fatalf("mkdir remote: %v", err)
	}
	run(remoteDir, "init", "--bare", "-q")

	// Source repo + one commit + origin wired.
	run(workDir, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(workDir, "README.md"), []byte("hi\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	run(workDir, "add", "-A")
	run(workDir, "commit", "-m", "init", "-q")
	run(workDir, "remote", "add", "origin", remoteDir)

	return workDir, remoteDir
}

func TestHandleLocalGitPush_HappyPath(t *testing.T) {
	workDir, _ := gitRepoFixture(t)

	stateDir := t.TempDir()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname: "myproject", Mode: topology.PlanModeLocalStage,
		StageHostname: "apistage", BootstrappedAt: "2026-04-01",
		// Phase 4 R-state: tests target downstream failure modes; the meta
		// represents a service that has gone through git-push-setup so the
		// pre-flight gate passes.
		CloseDeployMode: topology.CloseModeGitPush,
		GitPushState:    topology.GitPushConfigured,
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	result, _, err := handleLocalGitPush(
		context.Background(), nil, "proj-test", auth.Info{Email: "t@t.com", FullName: "test"},
		DeployLocalInput{
			TargetService: "myproject",
			WorkingDir:    workDir,
			Strategy:      deployStrategyGitPush,
			Branch:        "main",
		},
		stateDir,
	)
	if err != nil {
		t.Fatalf("handleLocalGitPush: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected push to succeed; got: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	for _, want := range []string{"PUSHED", "main"} {
		if !strings.Contains(text, want) {
			t.Errorf("result missing %q; got:\n%s", want, text)
		}
	}
}

func TestHandleLocalGitPush_NotAGitRepo_Refuses(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	workDir := t.TempDir() // no git init
	stateDir := t.TempDir()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname: "myproject", Mode: topology.PlanModeLocalOnly, BootstrappedAt: "2026-04-01",
		CloseDeployMode: topology.CloseModeGitPush,
		GitPushState:    topology.GitPushConfigured,
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	result, _, _ := handleLocalGitPush(
		context.Background(), nil, "proj-test", auth.Info{},
		DeployLocalInput{
			TargetService: "myproject",
			WorkingDir:    workDir,
			Strategy:      deployStrategyGitPush,
		},
		stateDir,
	)
	if !result.IsError {
		t.Fatalf("expected error for non-git workingDir; got: %s", getTextContent(t, result))
	}
	if !strings.Contains(getTextContent(t, result), "not a git repository") {
		t.Errorf("error should identify the specific failure; got:\n%s", getTextContent(t, result))
	}
}

func TestHandleLocalGitPush_NoOriginNoRemoteURL_Refuses(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	root := t.TempDir()
	workDir := filepath.Join(root, "work")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	run := func(args ...string) {
		//nolint:gosec // test-only, inputs are t.TempDir paths
		cmd := exec.CommandContext(context.Background(), "git", append([]string{"-C", workDir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=t@t.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=t@t.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(workDir, "README"), []byte("hi"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	run("add", "-A")
	run("commit", "-m", "hi", "-q")

	stateDir := t.TempDir()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname: "myproject", Mode: topology.PlanModeLocalOnly, BootstrappedAt: "2026-04-01",
		CloseDeployMode: topology.CloseModeGitPush,
		GitPushState:    topology.GitPushConfigured,
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	result, _, _ := handleLocalGitPush(
		context.Background(), nil, "proj-test", auth.Info{},
		DeployLocalInput{
			TargetService: "myproject",
			WorkingDir:    workDir,
			Strategy:      deployStrategyGitPush,
		},
		stateDir,
	)
	if !result.IsError {
		t.Fatalf("expected error when origin is missing AND no remoteUrl; got: %s", getTextContent(t, result))
	}
	if !strings.Contains(getTextContent(t, result), "no origin") {
		t.Errorf("error should call out the missing origin; got:\n%s", getTextContent(t, result))
	}
}

func TestHandleLocalGitPush_RemoteURLMismatch_Refuses(t *testing.T) {
	workDir, existingRemote := gitRepoFixture(t)
	stateDir := t.TempDir()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname: "myproject", Mode: topology.PlanModeLocalStage,
		StageHostname: "apistage", BootstrappedAt: "2026-04-01",
		// Phase 4 R-state: tests target downstream failure modes; the meta
		// represents a service that has gone through git-push-setup so the
		// pre-flight gate passes.
		CloseDeployMode: topology.CloseModeGitPush,
		GitPushState:    topology.GitPushConfigured,
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	result, _, _ := handleLocalGitPush(
		context.Background(), nil, "proj-test", auth.Info{},
		DeployLocalInput{
			TargetService: "myproject",
			WorkingDir:    workDir,
			Strategy:      deployStrategyGitPush,
			RemoteURL:     "https://example.com/other/repo.git",
		},
		stateDir,
	)
	if !result.IsError {
		t.Fatalf("expected error on origin mismatch (existing=%s, provided=other); got: %s", existingRemote, getTextContent(t, result))
	}
	if !strings.Contains(getTextContent(t, result), "won't silently rewrite") {
		t.Errorf("error should explicitly refuse silent rewrite; got:\n%s", getTextContent(t, result))
	}
}
