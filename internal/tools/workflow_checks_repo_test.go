// Tests for: tools/workflow_checks.go's repo-init check — the provision
// StepChecker addition that gates bootstrap completion on a repo being
// present at /var/www (container) or CWD (local), per
// docs/spec-workflows.md §4.10, G1.
package tools

import (
	"context"
	"os"
	"os/exec"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/runtime"
	"github.com/zeropsio/zcp/internal/workflow"
)

func repoInitPlan() *workflow.ServicePlan {
	return &workflow.ServicePlan{
		Targets: []workflow.BootstrapTarget{{
			Runtime: workflow.RuntimeTarget{DevHostname: "appdev", Type: "nodejs@22", BootstrapMode: "standard"},
		}},
	}
}

func TestCheckRepoInit_Container_SSHFails_ReportsFail(t *testing.T) {
	t.Parallel()
	ssh := &stubSSH{err: errWorkflowChecksRepo}
	rt := runtime.Info{InContainer: true}
	checks := checkRepoInit(context.Background(), ssh, rt, repoInitPlan())
	if len(checks) != 1 {
		t.Fatalf("checks = %+v, want 1", checks)
	}
	if checks[0].Status != statusFail {
		t.Errorf("status = %q, want fail", checks[0].Status)
	}
}

func TestCheckRepoInit_Container_SSHSucceeds_ReportsPass(t *testing.T) {
	t.Parallel()
	ssh := &stubSSH{output: []byte("")}
	rt := runtime.Info{InContainer: true}
	checks := checkRepoInit(context.Background(), ssh, rt, repoInitPlan())
	if len(checks) != 1 {
		t.Fatalf("checks = %+v, want 1", checks)
	}
	if checks[0].Status != statusPass {
		t.Errorf("status = %q, want pass: %+v", checks[0].Status, checks[0])
	}
}

func TestCheckRepoInit_Local_NotARepo_ReportsFailWithGitInitGuidance(t *testing.T) {
	t.Parallel()
	rt := runtime.Info{InContainer: false}
	dir := t.TempDir()
	checks := checkRepoInitAt(context.Background(), nil, rt, repoInitPlan(), dir)
	if len(checks) != 1 {
		t.Fatalf("checks = %+v, want 1", checks)
	}
	if checks[0].Status != statusFail {
		t.Errorf("status = %q, want fail (not a repo)", checks[0].Status)
	}
	if checks[0].Detail == "" {
		t.Error("Detail is empty, want git init guidance")
	}
}

func TestCheckRepoInit_Local_IsARepo_ReportsPass(t *testing.T) {
	t.Parallel()
	rt := runtime.Info{InContainer: false}
	dir := t.TempDir()
	runGit := func(args ...string) {
		cmd := exec.CommandContext(context.Background(), "git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	runGit("init", "-q", "-b", "main")
	runGit("commit", "-q", "--allow-empty", "-m", "init")

	checks := checkRepoInitAt(context.Background(), nil, rt, repoInitPlan(), dir)
	if len(checks) != 1 {
		t.Fatalf("checks = %+v, want 1", checks)
	}
	if checks[0].Status != statusPass {
		t.Errorf("status = %q, want pass: %+v", checks[0].Status, checks[0])
	}
}

func TestBuildStepChecker_Provision_MergesRepoInitCheck(t *testing.T) {
	t.Parallel()
	mock := platform.NewMock()
	ssh := &stubSSH{output: []byte("")}
	checker := buildStepChecker("provision", mock, nil, "proj-1", nil, nil, t.TempDir(), ssh, runtime.Info{InContainer: true})
	if checker == nil {
		t.Fatal("expected non-nil checker for provision")
	}
	result, err := checker(context.Background(), repoInitPlan(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	found := false
	for _, c := range result.Checks {
		if c.Name == "appdev_repo" {
			found = true
		}
	}
	if !found {
		t.Errorf("checks = %+v, want an appdev_repo check", result.Checks)
	}
}

var errWorkflowChecksRepo = &workflowChecksRepoTestError{"ssh failed"}

type workflowChecksRepoTestError struct{ msg string }

func (e *workflowChecksRepoTestError) Error() string { return e.msg }
