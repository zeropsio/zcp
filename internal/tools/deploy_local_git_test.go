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

// TestHandleLocalGitPush_DirtyTree_Warns pins F1a parity on the LOCAL path:
// an uncommitted/untracked file yields the shared dirty-tree warning (git-push
// transmits the committed HEAD only; it does not stage or commit). The warning
// fires even on a successful PUSHED outcome, since the dropped file is dropped
// regardless of whether the committed HEAD moved. Previously unpinned.
func TestHandleLocalGitPush_DirtyTree_Warns(t *testing.T) {
	workDir, _ := gitRepoFixture(t)
	// File the user "wrote" but never committed — git-push must not drop it
	// silently.
	if err := os.WriteFile(filepath.Join(workDir, "uncommitted.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatalf("write uncommitted: %v", err)
	}

	stateDir := t.TempDir()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname: "myproject", Mode: topology.PlanModeLocalStage,
		StageHostname: "apistage", BootstrappedAt: "2026-04-01",
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
		t.Fatalf("dirty tree must not error; got: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	for _, want := range []string{"uncommitted.txt", "committed HEAD"} {
		if !strings.Contains(text, want) {
			t.Errorf("expected dirty-tree warning to contain %q; got:\n%s", want, text)
		}
	}
}

// TestHandleLocalGitPush_DoesNotStampDeployed pins C2 closure (audit-
// prerelease-internal-testing-2026-04-29). The pre-fix path stamped
// FirstDeployedAt synchronously on git-push success, racing ahead of
// the actual async build landing. After the fix, push success records
// an in-flight DeployAttempt (no SucceededAt) and the response carries
// NextActions guidance pointing the agent at zerops_events +
// record-deploy. FirstDeployedAt must NOT be stamped at this stage.
func TestHandleLocalGitPush_DoesNotStampDeployed(t *testing.T) {
	workDir, _ := gitRepoFixture(t)
	stateDir := t.TempDir()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname: "myproject", Mode: topology.PlanModeLocalStage,
		StageHostname: "apistage", BootstrappedAt: "2026-04-01",
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
		t.Fatalf("expected push success; got: %s", getTextContent(t, result))
	}

	// FirstDeployedAt must remain empty after a successful git-push —
	// the actual deploy hasn't landed yet (build is async); only
	// record-deploy after Status=ACTIVE should stamp.
	meta, err := workflow.ReadServiceMeta(stateDir, "myproject")
	if err != nil {
		t.Fatalf("ReadServiceMeta: %v", err)
	}
	if meta == nil {
		t.Fatal("expected meta to exist")
	}
	if meta.FirstDeployedAt != "" {
		t.Errorf("FirstDeployedAt = %q, want empty (git-push must not auto-stamp; record-deploy is the bridge)", meta.FirstDeployedAt)
	}

	// Response must carry NextActions naming record-deploy as the bridge.
	// F2/F3 fix (round-3 audit): also pin the canonical events arg name
	// so a `filterServices=` typo regression surfaces as a test failure.
	text := getTextContent(t, result)
	for _, want := range []string{
		"record-deploy",
		"zerops_events",
		"Status=ACTIVE",
		"serviceHostname=",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("response missing %q in NextActions; got:\n%s", want, text)
		}
	}
	if strings.Contains(text, "filterServices") {
		t.Errorf("response uses retired arg `filterServices` (events tool only accepts serviceHostname):\n%s", text)
	}
}

// TestHandleLocalGitPush_NoFutureBuild_NeverRecordsDanglingAttempt mirrors
// GF-13's container-path fix (deploy_git_push.go) on the local path: a
// local git-push has no build watch at all — it only pushes bytes — so an
// in-flight DeployAttempt under the push source is honest only when a
// wired BuildIntegration gives it a resolver (the manual record-deploy
// bridge, after the agent observes Status=ACTIVE). No BuildIntegration
// wired has no resolver, so recording the placeholder there left a
// permanent, unexplained Success:false "failed deploy" on the push source
// — same bug class as the container path's, observed live on a Gitea-wired
// pair. Unlike the container path, a local git-push never targets this
// Mate's own Gitea — deploy_local_git.go never opens a pull request; there
// is no Mate on a developer's own machine — so there is no gitea-remote
// case to gate on here.
//
// The companion NOTHING_TO_PUSH branch is pinned separately, by
// TestLocalGitPushTrackable directly against the gate function: a real
// second push here can't reach that status because runGitWithEnv (this
// file) only captures stdout, and `git push` writes "Everything
// up-to-date" to stderr — a separate, pre-existing detection gap, not
// touched by this fix.
func TestHandleLocalGitPush_NoFutureBuild_NeverRecordsDanglingAttempt(t *testing.T) {
	workDir, _ := gitRepoFixture(t)
	stateDir := t.TempDir()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname: "myproject", Mode: topology.PlanModeLocalStage,
		StageHostname:   "apistage",
		BootstrappedAt:  "2026-04-01",
		CloseDeployMode: topology.CloseModeGitPush,
		GitPushState:    topology.GitPushConfigured,
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
	ws := workflow.NewWorkSession("proj-test", string(workflow.EnvLocal), "ship it", []string{"myproject"})
	if err := workflow.SaveWorkSession(stateDir, ws); err != nil {
		t.Fatalf("SaveWorkSession: %v", err)
	}
	t.Cleanup(func() { _ = workflow.DeleteWorkSession(stateDir, os.Getpid()) })

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
		t.Fatalf("expected push success; got: %s", getTextContent(t, result))
	}

	loaded, err := workflow.LoadWorkSession(stateDir, os.Getpid())
	if err != nil {
		t.Fatalf("LoadWorkSession: %v", err)
	}
	if attempts := loaded.Deploys["myproject"]; len(attempts) != 0 {
		t.Errorf("Deploys[myproject] = %+v, want none: nothing will ever resolve this attempt", attempts)
	}
}

// TestLocalGitPushTrackable pins the gate itself, table-driven: recording
// requires BOTH a wired BuildIntegration (the local path's only resolver —
// no build watch runs here) AND an actual transmission. Exercised directly
// because the NOTHING_TO_PUSH status is unreachable through a real `git
// push` in this test file today (see the note on
// TestHandleLocalGitPush_NoFutureBuild_NeverRecordsDanglingAttempt).
func TestLocalGitPushTrackable(t *testing.T) {
	tests := []struct {
		name                   string
		status                 string
		buildIntegrationConfig bool
		want                   bool
	}{
		{"pushed, integration wired", "PUSHED", true, true},
		{"pushed, no integration", "PUSHED", false, false},
		{"nothing to push, integration wired", statusNothingToPush, true, false},
		{"nothing to push, no integration", statusNothingToPush, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := localGitPushTrackable(tt.status, tt.buildIntegrationConfig); got != tt.want {
				t.Errorf("localGitPushTrackable(%q, %v) = %v, want %v", tt.status, tt.buildIntegrationConfig, got, tt.want)
			}
		})
	}
}

// TestHandleLocalGitPush_WebhookIntegration_StillRecordsInFlightAttempt
// pins that the genuine trackable case is unchanged: a wired
// BuildIntegration plus a real push still records the in-flight
// placeholder that zerops_events + record-deploy bridges.
func TestHandleLocalGitPush_WebhookIntegration_StillRecordsInFlightAttempt(t *testing.T) {
	workDir, _ := gitRepoFixture(t)
	stateDir := t.TempDir()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname: "myproject", Mode: topology.PlanModeLocalStage,
		StageHostname:    "apistage",
		BootstrappedAt:   "2026-04-01",
		CloseDeployMode:  topology.CloseModeGitPush,
		GitPushState:     topology.GitPushConfigured,
		BuildIntegration: topology.BuildIntegrationWebhook,
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
	ws := workflow.NewWorkSession("proj-test", string(workflow.EnvLocal), "ship it", []string{"myproject"})
	if err := workflow.SaveWorkSession(stateDir, ws); err != nil {
		t.Fatalf("SaveWorkSession: %v", err)
	}
	t.Cleanup(func() { _ = workflow.DeleteWorkSession(stateDir, os.Getpid()) })

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
		t.Fatalf("expected push success; got: %s", getTextContent(t, result))
	}

	loaded, err := workflow.LoadWorkSession(stateDir, os.Getpid())
	if err != nil {
		t.Fatalf("LoadWorkSession: %v", err)
	}
	attempts := loaded.Deploys["myproject"]
	if len(attempts) != 1 {
		t.Fatalf("Deploys[myproject] = %+v, want exactly 1 in-flight attempt", attempts)
	}
	if attempts[0].SucceededAt != "" {
		t.Errorf("in-flight attempt should have no SucceededAt yet, got %+v", attempts[0])
	}
}

func TestHandleLocalGitPush_RecordsServesHTTPFromSetup(t *testing.T) {
	workDir, _ := gitRepoFixture(t)
	if err := os.WriteFile(filepath.Join(workDir, "zerops.yaml"), []byte(`zerops:
  - setup: worker
    build:
      base: php-nginx@8.4
      deployFiles: [.]
    run:
      start: php artisan queue:work
`), 0o644); err != nil {
		t.Fatalf("write zerops.yaml: %v", err)
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
	run("add", "zerops.yaml")
	run("commit", "-m", "add zerops yaml", "-q")

	stateDir := t.TempDir()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname:        "myproject",
		Mode:            topology.PlanModeLocalOnly,
		BootstrappedAt:  "2026-04-01",
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
			Setup:         "worker",
			Branch:        "main",
		},
		stateDir,
	)
	if err != nil {
		t.Fatalf("handleLocalGitPush: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected push success; got: %s", getTextContent(t, result))
	}

	meta, err := workflow.FindServiceMeta(stateDir, "myproject")
	if err != nil {
		t.Fatalf("FindServiceMeta: %v", err)
	}
	if meta == nil {
		t.Fatal("expected service meta")
	}
	if meta.PrimarySetupName != "worker" {
		t.Errorf("PrimarySetupName = %q, want worker", meta.PrimarySetupName)
	}
	if meta.ServesHTTP == nil {
		t.Fatal("ServesHTTP = nil, want recorded bool")
	}
	if *meta.ServesHTTP {
		t.Errorf("ServesHTTP = true, want false for worker setup without ports")
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

// TestHandleLocalGitPush_RemoteURLDotGitOnlyDiffers_NoMismatch pins that a
// ".git"/slash-only difference between the configured origin and the passed
// remoteUrl is the SAME repo, not drift — the mismatch check compares repo
// IDENTITY via topology.CanonicalRepoURL, not raw bytes (mirrors
// TestValidateLaunchSourceControl_RemoteDotGitDiffersOnly_NoBlock on the
// launch-gate path). Without canonicalization this false-blocks a legitimate
// push (origin carries the conventional ".git" suffix, the agent passes
// remoteUrl without it).
func TestHandleLocalGitPush_RemoteURLDotGitOnlyDiffers_NoMismatch(t *testing.T) {
	workDir, existingRemote := gitRepoFixture(t)
	// Same repo as the wired origin, just without the ".git" suffix.
	sameRepoNoSuffix := strings.TrimSuffix(existingRemote, ".git")

	stateDir := t.TempDir()
	if err := workflow.WriteServiceMeta(stateDir, &workflow.ServiceMeta{
		Hostname: "myproject", Mode: topology.PlanModeLocalStage,
		StageHostname: "apistage", BootstrappedAt: "2026-04-01",
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
			RemoteURL:     sameRepoNoSuffix,
		},
		stateDir,
	)
	if err != nil {
		t.Fatalf("handleLocalGitPush: %v", err)
	}
	if result.IsError {
		t.Fatalf("a .git-only difference must NOT refuse as a mismatch; got: %s", getTextContent(t, result))
	}
	text := getTextContent(t, result)
	if strings.Contains(text, "won't silently rewrite") {
		t.Errorf("a .git-only difference must not trigger the mismatch refusal; got:\n%s", text)
	}
	if !strings.Contains(text, "PUSHED") {
		t.Errorf("expected push to succeed; got:\n%s", text)
	}
}
