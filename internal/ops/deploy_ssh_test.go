// Tests for: ops/deploy.go — Deploy with SSH mode (SSH-only, no local deploy).
package ops

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/ops/git"
	"github.com/zeropsio/zcp/internal/platform"
)

type sshCall struct {
	hostname string
	command  string
	// background is true when the call came through ExecSSHBackground.
	background bool
	// bgTimeout carries the timeout the caller passed to the background
	// variant. Zero for foreground calls.
	bgTimeout time.Duration
}

type mockSSHDeployer struct {
	output []byte
	err    error
	// bgOutput and bgErr override the defaults for background calls so
	// tests can drive a spawn_timeout or spawn_error path independently
	// from the foreground outputs.
	bgOutput []byte
	bgErr    error
	calls    []sshCall
	// results, when non-empty, is consumed in call order — one entry per
	// ExecSSH call — for tests (sha resolve/mktemp/extract/push) that need
	// a different return per round trip. Falls back to output/err once
	// exhausted, or entirely when unset, so every other test keeps its
	// single static output.
	results []sshResult
	callIdx int
}

type sshResult struct {
	output []byte
	err    error
}

func (m *mockSSHDeployer) ExecSSH(_ context.Context, hostname, command string) ([]byte, error) {
	m.calls = append(m.calls, sshCall{hostname: hostname, command: command})
	if m.callIdx < len(m.results) {
		r := m.results[m.callIdx]
		m.callIdx++
		return r.output, r.err
	}
	return m.output, m.err
}

func (m *mockSSHDeployer) ExecSSHBackground(_ context.Context, hostname, command string, timeout time.Duration) ([]byte, error) {
	m.calls = append(m.calls, sshCall{hostname: hostname, command: command, background: true, bgTimeout: timeout})
	if m.bgOutput != nil || m.bgErr != nil {
		return m.bgOutput, m.bgErr
	}
	return m.output, m.err
}

func testAuthInfo() auth.Info {
	return auth.Info{
		Token:    "test-token",
		APIHost:  "api.app-prg1.zerops.io",
		Region:   "prg1",
		Email:    "test@example.com",
		FullName: "Test User",
	}
}

func TestDeploy_SSHMode_Success(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		sourceService string
		targetService string
		workingDir    string
		wantMode      string
	}{
		{
			name:          "ssh basic",
			sourceService: "builder",
			targetService: "app",
			wantMode:      "ssh",
		},
		{
			name:          "ssh with workingDir",
			sourceService: "builder",
			targetService: "app",
			workingDir:    "/opt/app",
			wantMode:      "ssh",
		},
		{
			name:          "ssh default workingDir",
			sourceService: "builder",
			targetService: "app",
			wantMode:      "ssh",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mock := platform.NewMock().
				WithServices([]platform.ServiceStack{
					{ID: "svc-1", Name: "builder"},
					{ID: "svc-2", Name: "app"},
				})
			ssh := &mockSSHDeployer{output: []byte("ok")}
			authInfo := testAuthInfo()

			result, err := DeploySSH(context.Background(), mock, "proj-1", ssh, authInfo,
				tt.sourceService, tt.targetService, "", tt.workingDir, "")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.Status != "BUILD_TRIGGERED" {
				t.Errorf("status = %s, want BUILD_TRIGGERED", result.Status)
			}
			if result.MonitorHint == "" {
				t.Error("monitorHint should not be empty")
			}
			if result.Mode != tt.wantMode {
				t.Errorf("mode = %s, want %s", result.Mode, tt.wantMode)
			}
			if result.TargetService != tt.targetService {
				t.Errorf("targetService = %s, want %s", result.TargetService, tt.targetService)
			}
			if result.SourceService != tt.sourceService {
				t.Errorf("sourceService = %s, want %s", result.SourceService, tt.sourceService)
			}
			// 3 calls: HeadStatus (item 4's working-
			// tree recording — the mock's canned "ok" output satisfies
			// HeadStatus's rev-parse, so the source "has a repo" from the
			// mock's perspective) + the push itself + its build wait.
			if len(ssh.calls) != 3 {
				t.Fatalf("ssh calls = %d, want 3", len(ssh.calls))
			}
			for _, c := range ssh.calls {
				if c.hostname != "builder" {
					t.Errorf("ssh hostname = %s, want builder", c.hostname)
				}
			}
		})
	}
}

func TestDeploy_SSHMode_SourceNotFound(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-2", Name: "app"},
		})
	ssh := &mockSSHDeployer{}
	authInfo := testAuthInfo()

	_, err := DeploySSH(context.Background(), mock, "proj-1", ssh, authInfo,
		"nonexistent", "app", "", "", "")
	if err == nil {
		t.Fatal("expected error for nonexistent source service")
	}

	var pe *platform.PlatformError
	if !errorAs(err, &pe) {
		t.Fatalf("expected PlatformError, got %T: %v", err, err)
	}
	if pe.Code != platform.ErrServiceNotFound {
		t.Errorf("code = %s, want %s", pe.Code, platform.ErrServiceNotFound)
	}
}

func TestDeploy_SSHMode_TargetNotFound(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "builder"},
		})
	ssh := &mockSSHDeployer{}
	authInfo := testAuthInfo()

	_, err := DeploySSH(context.Background(), mock, "proj-1", ssh, authInfo,
		"builder", "nonexistent", "", "", "")
	if err == nil {
		t.Fatal("expected error for nonexistent target service")
	}
}

// TestDeploy_SSHMode_MountStyleWorkingDirSuggestsSourceService pins the
// suggestion text on the workingDir gate. The gate rejects any
// workingDir starting with /var/www/ (mount path on the dev container,
// not a valid target-runtime path). The original suggestion only said
// "Use /var/www or omit" — agents (Gemini 2026-05-24 audit) hit this
// and self-corrected by passing sourceService=<hostname>. The
// suggestion now spells out that recovery pattern so a future agent
// can fix in one shot instead of guessing.
func TestDeploy_SSHMode_MountStyleWorkingDirSuggestsSourceService(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "weatherbun"},
			{ID: "svc-2", Name: "weatherbun"}, // self-deploy: source==target
		})
	ssh := &mockSSHDeployer{}
	authInfo := testAuthInfo()

	_, err := DeploySSH(context.Background(), mock, "proj-1", ssh, authInfo,
		"weatherbun", "weatherbun", "", "/var/www/weatherbun", "")
	if err == nil {
		t.Fatal("expected error for mount-style workingDir")
	}

	var pe *platform.PlatformError
	if !errorAs(err, &pe) {
		t.Fatalf("expected PlatformError, got %T: %v", err, err)
	}
	if pe.Code != platform.ErrInvalidParameter {
		t.Errorf("code = %s, want %s", pe.Code, platform.ErrInvalidParameter)
	}
	if !containsSubstring(pe.Message, "looks like a local SSHFS mount path") {
		t.Errorf("message should describe the mount-path heuristic, got: %s", pe.Message)
	}
	// Suggestion must name BOTH escape hatches — the working recovery
	// pattern (omit workingDir + sourceService=<hostname>) and the
	// minimal fix (workingDir="/var/www").
	if !containsSubstring(pe.Suggestion, `sourceService="weatherbun"`) {
		t.Errorf("suggestion should propose sourceService=\"weatherbun\" recovery, got: %s", pe.Suggestion)
	}
	if !containsSubstring(pe.Suggestion, `workingDir="/var/www"`) {
		t.Errorf("suggestion should also mention workingDir=\"/var/www\" fallback, got: %s", pe.Suggestion)
	}
}

func TestDeploy_SSHMode_SSHError(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "builder"},
			{ID: "svc-2", Name: "app"},
		})
	ssh := &mockSSHDeployer{err: fmt.Errorf("connection refused")}
	authInfo := testAuthInfo()

	_, err := DeploySSH(context.Background(), mock, "proj-1", ssh, authInfo,
		"builder", "app", "", "", "")
	if err == nil {
		t.Fatal("expected error for SSH failure")
	}

	var pe *platform.PlatformError
	if !errorAs(err, &pe) {
		t.Fatalf("expected PlatformError, got %T: %v", err, err)
	}
	if pe.Code != platform.ErrSSHDeployFailed {
		t.Errorf("code = %s, want %s", pe.Code, platform.ErrSSHDeployFailed)
	}
}

func TestDeploy_SSHMode_SignalKilled(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "builder"},
			{ID: "svc-2", Name: "app"},
		})
	ssh := &mockSSHDeployer{
		output: []byte("building...\nKilled"),
		err:    &platform.SSHExecError{Hostname: "builder", Output: "building...\nKilled", Err: fmt.Errorf("signal: killed")},
	}
	authInfo := testAuthInfo()

	_, err := DeploySSH(context.Background(), mock, "proj-1", ssh, authInfo,
		"builder", "app", "", "", "")
	if err == nil {
		t.Fatal("expected error for signal killed")
	}

	var pe *platform.PlatformError
	if !errorAs(err, &pe) {
		t.Fatalf("expected PlatformError, got %T: %v", err, err)
	}
	if pe.Code != platform.ErrSSHDeployFailed {
		t.Errorf("code = %s, want %s", pe.Code, platform.ErrSSHDeployFailed)
	}
	if !containsSubstring(pe.Message, "OOM") {
		t.Errorf("message should mention OOM, got: %s", pe.Message)
	}
}

func TestDeploy_SSHMode_CommandNotFound(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "builder"},
			{ID: "svc-2", Name: "app"},
		})
	ssh := &mockSSHDeployer{
		output: []byte("bash: zcli: command not found"),
		err:    &platform.SSHExecError{Hostname: "builder", Output: "bash: zcli: command not found", Err: fmt.Errorf("exit status 127")},
	}
	authInfo := testAuthInfo()

	_, err := DeploySSH(context.Background(), mock, "proj-1", ssh, authInfo,
		"builder", "app", "", "", "")
	if err == nil {
		t.Fatal("expected error for command not found")
	}

	var pe *platform.PlatformError
	if !errorAs(err, &pe) {
		t.Fatalf("expected PlatformError, got %T: %v", err, err)
	}
	if pe.Code != platform.ErrSSHDeployFailed {
		t.Errorf("code = %s, want %s", pe.Code, platform.ErrSSHDeployFailed)
	}
	// "command not found" should appear in the raw output shown to LLM.
	if !containsSubstring(pe.Message, "command not found") {
		t.Errorf("message should contain raw error text, got: %s", pe.Message)
	}
}

func TestDeploy_SSHMode_GenericError(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "builder"},
			{ID: "svc-2", Name: "app"},
		})
	ssh := &mockSSHDeployer{err: fmt.Errorf("some unexpected error")}
	authInfo := testAuthInfo()

	_, err := DeploySSH(context.Background(), mock, "proj-1", ssh, authInfo,
		"builder", "app", "", "", "")
	if err == nil {
		t.Fatal("expected error for generic SSH failure")
	}

	var pe *platform.PlatformError
	if !errorAs(err, &pe) {
		t.Fatalf("expected PlatformError, got %T: %v", err, err)
	}
	if pe.Code != platform.ErrSSHDeployFailed {
		t.Errorf("code = %s, want %s", pe.Code, platform.ErrSSHDeployFailed)
	}
}

func TestDeploy_SSHMode_WithRegion(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "builder"},
			{ID: "svc-2", Name: "app"},
		})
	ssh := &mockSSHDeployer{output: []byte("ok")}
	authInfo := auth.Info{
		Token:   "test-token",
		APIHost: "api.app-fra1.zerops.io",
		Region:  "fra1",
	}

	result, err := DeploySSH(context.Background(), mock, "proj-1", ssh, authInfo,
		"builder", "app", "", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Mode != "ssh" {
		t.Errorf("mode = %s, want ssh", result.Mode)
	}
	// Verify the token rides the push's environment, without
	// --zeropsRegion. 3 calls: the self-deploy preflight read (item 4) +
	// the push itself + its build wait.
	if len(ssh.calls) != 3 {
		t.Fatalf("ssh calls = %d, want 3", len(ssh.calls))
	}
	cmd := ssh.calls[1].command
	if !containsSubstring(cmd, "ZEROPS_TOKEN='test-token' zcli push") {
		t.Errorf("SSH command should pass the token through the push env, got: %s", cmd)
	}
	if containsSubstring(cmd, "--zeropsRegion") {
		t.Errorf("SSH command should NOT contain '--zeropsRegion', got: %s", cmd)
	}
}

func TestDeploy_SSHMode_Exit255WithBuildSuccess(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		output string
	}{
		{
			name:   "build artefacts ready marker",
			output: "Uploading files...\nBUILD ARTEFACTS READY TO DEPLOY\nConnection to host closed by remote host.\n",
		},
		{
			name:   "deploying service marker",
			output: "zcli push completed\nDeploying service stack svc-2...\nConnection closed.\n",
		},
		{
			name:   "both markers present",
			output: "BUILD ARTEFACTS READY TO DEPLOY\nDeploying service stack svc-2\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mock := platform.NewMock().
				WithServices([]platform.ServiceStack{
					{ID: "svc-1", Name: "builder"},
					{ID: "svc-2", Name: "app"},
				})
			ssh := &mockSSHDeployer{
				output: []byte(tt.output),
				err:    &platform.SSHExecError{Hostname: "builder", Output: tt.output, Err: fmt.Errorf("process exited with status 255")},
			}
			authInfo := testAuthInfo()

			result, err := DeploySSH(context.Background(), mock, "proj-1", ssh, authInfo,
				"builder", "app", "", "", "")
			if err != nil {
				t.Fatalf("expected success (build triggered recovery), got error: %v", err)
			}
			if result.Status != "BUILD_TRIGGERED" {
				t.Errorf("status = %s, want BUILD_TRIGGERED", result.Status)
			}
			if result.Mode != "ssh" {
				t.Errorf("mode = %s, want ssh", result.Mode)
			}
			if result.TargetServiceID != "svc-2" {
				t.Errorf("targetServiceID = %s, want svc-2", result.TargetServiceID)
			}
		})
	}
}

func TestDeploy_SSHMode_Exit255RealFailure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		output string
	}{
		{
			name:   "no build markers",
			output: "Error: File zerops.yml not found\n",
		},
		{
			name:   "empty output",
			output: "",
		},
		{
			name:   "generic failure output",
			output: "fatal: could not read Username\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mock := platform.NewMock().
				WithServices([]platform.ServiceStack{
					{ID: "svc-1", Name: "builder"},
					{ID: "svc-2", Name: "app"},
				})
			ssh := &mockSSHDeployer{
				output: []byte(tt.output),
				err:    &platform.SSHExecError{Hostname: "builder", Output: tt.output, Err: fmt.Errorf("process exited with status 255")},
			}
			authInfo := testAuthInfo()

			_, err := DeploySSH(context.Background(), mock, "proj-1", ssh, authInfo,
				"builder", "app", "", "", "")
			if err == nil {
				t.Fatal("expected error for exit 255 without build success markers")
			}

			var pe *platform.PlatformError
			if !errorAs(err, &pe) {
				t.Fatalf("expected PlatformError, got %T: %v", err, err)
			}
			if pe.Code != platform.ErrSSHDeployFailed {
				t.Errorf("code = %s, want %s", pe.Code, platform.ErrSSHDeployFailed)
			}
		})
	}
}

// Classification tests moved to deploy_classify_test.go.

func TestIsSSHBuildTriggered(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		output string
		want   bool
	}{
		{
			name:   "build artefacts marker",
			output: "Uploading...\nBUILD ARTEFACTS READY TO DEPLOY\ndone",
			want:   true,
		},
		{
			name:   "deploying service marker",
			output: "Deploying service stack svc-1...\n",
			want:   true,
		},
		{
			name:   "no markers",
			output: "Error: something went wrong",
			want:   false,
		},
		{
			name:   "empty output",
			output: "",
			want:   false,
		},
		{
			name:   "case sensitivity - lowercase should not match",
			output: "build artefacts ready to deploy",
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := isSSHBuildTriggered(tt.output); got != tt.want {
				t.Errorf("isSSHBuildTriggered(%q) = %v, want %v", tt.output, got, tt.want)
			}
		})
	}
}

// TestBuildSSHCommand_Shape locks the canonical shape of the command:
// gitInit guarded by `(test -d .git || ...)` (only runs when needed),
// identity ensure top-level, set-if-absent (runs even when .git/
// pre-exists from buildFromGit / upstream clone — B13, but never stomps a
// value already there — P1), then the HEAD guarantee, then push. No
// `git add` / `git commit` — direct deploy never mints a commit (P2).
// Identity lands from the DeployGitIdentity package constant — not from a
// caller — so shell escaping of user-controlled strings no longer
// applies. shellQuote itself is exercised by TestShellQuote below.
func TestBuildSSHCommand_Shape(t *testing.T) {
	t.Parallel()

	authInfo := auth.Info{
		Token:   "test-token",
		APIHost: "api.app-prg1.zerops.io",
		Region:  "prg1",
	}
	cmd := buildSSHCommand(authInfo, "svc-target", "/var/www", "", false, "", "/tmp/zcp-push-test")

	wantContains := []string{
		"ZEROPS_TOKEN='test-token' zcli push --service-id svc-target",
		"cd '/var/www'",
		"(test -d .git || git init -q -b main)",
		`(test -n "$(git config user.email)" || git config user.email 'agent@zerops.io') && (test -n "$(git config user.name)" || git config user.name 'Zerops Agent')`,
		`git rev-parse -q --verify HEAD >/dev/null || git update-ref HEAD`,
		"commit-tree",
		"-m 'zcp init'",
	}
	for _, want := range wantContains {
		if !containsSubstring(cmd, want) {
			t.Errorf("missing substring %q in command:\n%s", want, cmd)
		}
	}

	wantAbsent := []string{
		// gitConfig must NOT live inside the gitInit OR branch. The bug it
		// guards against (B13): a buildFromGit-provisioned service has
		// /var/www/.git/ from the upstream clone but no user.email/user.name
		// configured. Inside-OR config short-circuits via test -d, leaving
		// identity unset. Always-running config matches InitServiceGit.
		"(git init -q -b main && git config user.email",
		// P2: direct deploy never stages or commits — zcli's own archiver
		// snapshots the (possibly dirty) tree ephemerally.
		"git add -A",
		"git commit -q -m 'deploy'",
	}
	for _, absent := range wantAbsent {
		if containsSubstring(cmd, absent) {
			t.Errorf("command must NOT contain %q:\n%s", absent, cmd)
		}
	}
}

// TestBuildSSHCommand_VersionNameFromHead pins GF-10 (docs/spec-workflows.md
// §12.6): buildSSHCommand passes --version-name exactly when the caller
// supplies a non-empty versionName — clean (bare sha), dirty (sha with a
// "-dirty" suffix, computed by versionNameFor at the call site, not
// here), and unborn/no-repo (empty versionName ⇒ no flag at all).
func TestBuildSSHCommand_VersionNameFromHead(t *testing.T) {
	t.Parallel()
	authInfo := auth.Info{Token: "test-token"}

	t.Run("clean", func(t *testing.T) {
		t.Parallel()
		cmd := buildSSHCommand(authInfo, "svc-target", "/var/www", "", false, "fullhead1234567", "/tmp/zcp-push-test")
		if !containsSubstring(cmd, "--version-name 'fullhead1234567'") {
			t.Errorf("command missing --version-name 'fullhead1234567':\n%s", cmd)
		}
	})

	t.Run("dirty", func(t *testing.T) {
		t.Parallel()
		cmd := buildSSHCommand(authInfo, "svc-target", "/var/www", "", false, "fullhead1234567-dirty", "/tmp/zcp-push-test")
		if !containsSubstring(cmd, "--version-name 'fullhead1234567-dirty'") {
			t.Errorf("command missing --version-name 'fullhead1234567-dirty':\n%s", cmd)
		}
	})

	t.Run("unborn", func(t *testing.T) {
		t.Parallel()
		cmd := buildSSHCommand(authInfo, "svc-target", "/var/www", "", false, "", "/tmp/zcp-push-test")
		if containsSubstring(cmd, "--version-name") {
			t.Errorf("command must NOT carry --version-name when versionName is empty (no reachable HEAD):\n%s", cmd)
		}
	})
}

// TestVersionNameForHead pins the shared GF-10 formatting rule
// (docs/spec-workflows.md §12.6) both deploy transports (SSH, local) call:
// empty sha ⇒ empty (no reachable HEAD, omit the flag); non-empty +
// !dirty ⇒ bare sha; non-empty + dirty ⇒ sha with a "-dirty" suffix.
// TestVersionName is the name a zcp push of a working tree gives its app
// version (GF-10): a label and the commit's short sha, two tokens, as the Mate
// app and HQ read it — the branch HEAD is on, or HEAD when it is on
// none — with "-dirty" on the sha for uncommitted changes, which both read as
// no commit.
func TestVersionName(t *testing.T) {
	t.Parallel()
	const sha = "7E2D4C1a9b8c7d6e5f4a3b2c1d0e9f8a7b6c5d4e"
	for _, tt := range []struct {
		name   string
		branch string
		sha    string
		dirty  bool
		want   string
	}{
		{"a clean push on a branch", "main", sha, false, "main 7e2d4c1"},
		{"a working tree with uncommitted changes", "main", sha, true, "main 7e2d4c1-dirty"},
		{"a Mate's branch", "mate/mate-p1", sha, false, "mate/mate-p1 7e2d4c1"},
		{"HEAD on no branch", "", sha, false, "HEAD 7e2d4c1"},
		{"HEAD on no branch, dirty", "", sha, true, "HEAD 7e2d4c1-dirty"},
		{"a branch that would make a third token is no branch", "a b", sha, false, "HEAD 7e2d4c1"},
		{"no reachable HEAD names nothing", "main", "", false, ""},
		{"no reachable HEAD, dirty flag ignored", "main", "", true, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := versionNameFor(tt.branch, tt.sha, tt.dirty); got != tt.want {
				t.Errorf("versionNameFor(%q, %q, %v) = %q, want %q", tt.branch, tt.sha, tt.dirty, got, tt.want)
			}
		})
	}
}

// TestCommitVersionName is a deploy-from-commit's name: the checked-out
// branch when the commit is its tip, else "commit" — a label for people only;
// the short sha is the commit either way. Never dirty: it ships exactly the
// commit's tree.
func TestCommitVersionName(t *testing.T) {
	t.Parallel()
	const sha = "7e2d4c1a9b8c7d6e5f4a3b2c1d0e9f8a7b6c5d4e"
	const other = "0123456789abcdef0123456789abcdef01234567"
	for _, tt := range []struct {
		name                string
		headSHA, headBranch string
		want                string
	}{
		{"the tip of the checked-out branch", sha, "main", "main 7e2d4c1"},
		{"an older commit", other, "main", "commit 7e2d4c1"},
		{"HEAD detached at the commit", sha, "", "commit 7e2d4c1"},
		{"no HEAD read", "", "", "commit 7e2d4c1"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := commitVersionName(sha, tt.headSHA, tt.headBranch); got != tt.want {
				t.Errorf("commitVersionName = %q, want %q", got, tt.want)
			}
		})
	}
	if got := commitVersionName("", sha, "main"); got != "" {
		t.Errorf("no commit names nothing, got %q", got)
	}
}

// extractGitEnsureChain pulls the self-heal chain (cd ... init ...
// identity ... HEAD guarantee) out of buildSSHCommand's full output —
// everything up to (not including) the crew save and the `zcli push` after
// it. Running only this piece against a scratch dir is what a cold-path
// deploy would actually execute; the trailing `zcli push` would fail locally
// against a fake token and isn't the part these tests care about.
func extractGitEnsureChain(t *testing.T, dir string) string {
	t.Helper()
	authInfo := auth.Info{Token: "tok"}
	full := buildSSHCommand(authInfo, "svc-target", dir, "", false, "", "/tmp/zcp-push-test")
	chain, _, found := strings.Cut(full, " && "+crewSaveCommand)
	if !found {
		t.Fatalf("command missing `zcli push` anchor, shape drifted:\n%s", full)
	}
	return chain
}

// isolatedGitEnv returns an environment for real-git subprocesses that
// cannot see the developer machine's global/system git config — the
// set-if-absent probe (`test -n "$(git config user.email)"`) resolves the
// EFFECTIVE value (local, then global, then system), by design (identity
// must EXIST, from wherever — spec-git-delivery-target's B13 finding), so
// a real `~/.gitconfig` with an identity already set would silently make
// these tests pass for the wrong reason (or fail outright, as a stray
// global identity did the first time this test ran unisolated).
// GIT_CONFIG_NOSYSTEM covers /etc/gitconfig; the filtered/replaced HOME
// covers `~/.gitconfig`.
func isolatedGitEnv(t *testing.T) []string {
	t.Helper()
	home := t.TempDir()
	env := os.Environ()
	filtered := make([]string, 0, len(env)+2)
	for _, e := range env {
		switch {
		case strings.HasPrefix(e, "HOME="),
			strings.HasPrefix(e, "GIT_CONFIG_GLOBAL="),
			strings.HasPrefix(e, "GIT_CONFIG_SYSTEM="),
			strings.HasPrefix(e, "GIT_CONFIG_NOSYSTEM="):
			continue
		}
		filtered = append(filtered, e)
	}
	return append(filtered, "HOME="+home, "GIT_CONFIG_NOSYSTEM=1")
}

func runShellChain(t *testing.T, chain string, env []string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", "-c", chain)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git-ensure chain failed: %v\noutput: %s\ncommand: %s", err, out, chain)
	}
}

func mustRunGit(t *testing.T, dir string, env []string, args ...string) {
	t.Helper()
	//nolint:gosec // test-only, inputs are t.TempDir paths
	cmd := exec.CommandContext(context.Background(), "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\noutput: %s", args, err, out)
	}
}

func gitConfigGet(dir string, env []string, key string) string {
	cmd := exec.CommandContext(context.Background(), "git", "-C", dir, "config", "--get", key)
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func gitCommitCount(dir string, env []string) int {
	cmd := exec.CommandContext(context.Background(), "git", "-C", dir, "rev-list", "--count", "HEAD")
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	return n
}

func gitHeadSHA(dir string, env []string) string {
	cmd := exec.CommandContext(context.Background(), "git", "-C", dir, "rev-parse", "HEAD")
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func gitLogFormat(dir string, env []string, format string) string {
	//nolint:gosec // test-only, format is a literal from the test body
	cmd := exec.CommandContext(context.Background(), "git", "-C", dir, "log", "-1", "--format="+format)
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func gitPorcelain(dir string, env []string) string {
	cmd := exec.CommandContext(context.Background(), "git", "-C", dir, "status", "--porcelain")
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// TestBuildSSHCommand_FreshInitPath executes the git-ensure portion of the
// emitted command against a real git binary on scratch dirs, proving the
// self-heal chain leaves a commit-ready repo behind (P1: identity, P2: HEAD
// guarantee only — never a real commit) and never touches an already-healthy
// repo's history or dirty tree.
//
// Skipped under -short and when git is not on PATH.
func TestBuildSSHCommand_FreshInitPath(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping under -short; needs real git binary")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}

	t.Run("fresh repo gets robot identity and exactly one zcp init commit", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		env := isolatedGitEnv(t)
		runShellChain(t, extractGitEnsureChain(t, dir), env)

		if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
			t.Fatalf(".git not created: %v", err)
		}
		if got := gitConfigGet(dir, env, "user.email"); got != "agent@zerops.io" {
			t.Errorf("user.email = %q, want agent@zerops.io", got)
		}
		if got := gitConfigGet(dir, env, "user.name"); got != "Zerops Agent" {
			t.Errorf("user.name = %q, want Zerops Agent", got)
		}
		if n := gitCommitCount(dir, env); n != 1 {
			t.Errorf("commit count = %d, want exactly 1 (the zcp init marker)", n)
		}
		if msg := gitLogFormat(dir, env, "%s"); msg != "zcp init" {
			t.Errorf("HEAD message = %q, want %q", msg, "zcp init")
		}
	})

	t.Run("existing repo with dirty tree stays untouched", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		env := isolatedGitEnv(t)
		mustRunGit(t, dir, env, "init", "-q", "-b", "main")
		mustRunGit(t, dir, env, "config", "user.email", "custom@example.com")
		mustRunGit(t, dir, env, "config", "user.name", "Custom User")
		if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi"), 0o644); err != nil {
			t.Fatalf("seed file: %v", err)
		}
		mustRunGit(t, dir, env, "add", "-A")
		mustRunGit(t, dir, env, "commit", "-q", "-m", "seed")
		headBefore := gitHeadSHA(dir, env)

		// Dirty the tree AFTER the seed commit — this is the state a
		// mid-iteration container is normally in.
		if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("dirty edit"), 0o644); err != nil {
			t.Fatalf("dirty file: %v", err)
		}

		runShellChain(t, extractGitEnsureChain(t, dir), env)

		if got := gitHeadSHA(dir, env); got != headBefore {
			t.Errorf("HEAD moved: before=%s after=%s — the chain must not commit on an already-healthy repo", headBefore, got)
		}
		if n := gitCommitCount(dir, env); n != 1 {
			t.Errorf("commit count = %d, want still 1 — no new commit minted", n)
		}
		if porcelain := gitPorcelain(dir, env); porcelain == "" {
			t.Error("tree should still be dirty after the chain, got clean status")
		}
	})
}

// TestBuildSSHCommand_PresetIdentitySurvives is the P1 behavioral point:
// an operator-set identity on the container is NEVER stomped by the
// deploy safety-net, even though the HEAD guarantee still fires (an
// unborn repo still needs its marker commit) — and that marker commit
// carries the ROBOT identity inline via `-c`, not the surviving custom
// config, because it's ZCP's commit, not the user's.
func TestBuildSSHCommand_PresetIdentitySurvives(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping under -short; needs real git binary")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	t.Parallel()

	dir := t.TempDir()
	env := isolatedGitEnv(t)
	mustRunGit(t, dir, env, "init", "-q", "-b", "main")
	mustRunGit(t, dir, env, "config", "user.email", "custom@example.com")
	mustRunGit(t, dir, env, "config", "user.name", "Custom User")
	// No commit yet — HEAD is unborn.

	runShellChain(t, extractGitEnsureChain(t, dir), env)

	if got := gitConfigGet(dir, env, "user.email"); got != "custom@example.com" {
		t.Errorf("user.email was stomped: got %q, want custom@example.com to survive", got)
	}
	if got := gitConfigGet(dir, env, "user.name"); got != "Custom User" {
		t.Errorf("user.name was stomped: got %q, want Custom User to survive", got)
	}
	if n := gitCommitCount(dir, env); n != 1 {
		t.Fatalf("commit count = %d, want exactly 1 (the zcp init marker on the unborn repo)", n)
	}
	// The marker commit itself must carry the ROBOT identity (per-invocation
	// -c), proving it's independent of whatever repo-local config survives.
	if got := gitLogFormat(dir, env, "%an <%ae>"); got != "Zerops Agent <agent@zerops.io>" {
		t.Errorf("zcp init commit author = %q, want the robot identity (inline -c, independent of repo config)", got)
	}
}

func TestShellQuote(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "simple string",
			input: "hello",
			want:  "'hello'",
		},
		{
			name:  "single quote POSIX escape",
			input: "O'Brien",
			want:  "'O'\\''Brien'",
		},
		{
			name:  "dollar expansion neutralized",
			input: "$(whoami)",
			want:  "'$(whoami)'",
		},
		{
			name:  "backtick neutralized",
			input: "`id`",
			want:  "'`id`'",
		},
		{
			name:  "empty string",
			input: "",
			want:  "''",
		},
		{
			name:  "multiple single quotes",
			input: "it's a 'test'",
			want:  "'it'\\''s a '\\''test'\\'''",
		},
		{
			name:  "spaces preserved",
			input: "hello world",
			want:  "'hello world'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := shellQuote(tt.input)
			if got != tt.want {
				t.Errorf("shellQuote(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestDeploy_SelfDeploy_AutoInfer(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "app"},
		})
	ssh := &mockSSHDeployer{output: []byte("ok")}
	authInfo := testAuthInfo()

	// Only targetService provided, sourceService empty → auto-infer self-deploy.
	result, err := DeploySSH(context.Background(), mock, "proj-1", ssh, authInfo,
		"", "app", "", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Mode != "ssh" {
		t.Errorf("mode = %s, want ssh", result.Mode)
	}
	if result.SourceService != "app" {
		t.Errorf("sourceService = %s, want app (auto-inferred)", result.SourceService)
	}
	if result.TargetService != "app" {
		t.Errorf("targetService = %s, want app", result.TargetService)
	}
	// 3 calls: HeadStatus (item 4) + the push itself + its build wait.
	if len(ssh.calls) != 3 {
		t.Fatalf("ssh calls = %d, want 3", len(ssh.calls))
	}
	for _, c := range ssh.calls {
		if c.hostname != "app" {
			t.Errorf("ssh hostname = %s, want app", c.hostname)
		}
	}
}

// DeploySSH derives the -g flag from the source/target pair — self-deploy
// (source == target) gets -g so the service keeps its own .git.
func TestDeploy_SelfDeploy_IncludesGit(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "app"},
		})
	ssh := &mockSSHDeployer{output: []byte("ok")}
	authInfo := testAuthInfo()

	result, err := DeploySSH(context.Background(), mock, "proj-1", ssh, authInfo,
		"app", "app", "", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Mode != "ssh" {
		t.Errorf("mode = %s, want ssh", result.Mode)
	}
	// 3 calls: HeadStatus (item 4) + the push itself + its build wait.
	if len(ssh.calls) != 3 {
		t.Fatalf("ssh calls = %d, want 3", len(ssh.calls))
	}
	cmd := ssh.calls[1].command
	if !containsSubstring(cmd, " -g") {
		t.Errorf("SSH command should contain -g flag for self-deploy, got: %s", cmd)
	}
}

// Symmetric assertion: cross-deploy (source != target) must NOT include -g,
// otherwise the target service would inherit the source container's .git.
func TestDeploy_CrossDeploy_OmitsGit(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "builder"},
			{ID: "svc-2", Name: "app"},
		})
	ssh := &mockSSHDeployer{output: []byte("ok")}
	authInfo := testAuthInfo()

	_, err := DeploySSH(context.Background(), mock, "proj-1", ssh, authInfo,
		"builder", "app", "", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 3 calls: HeadStatus (item 4) + the push itself + its build wait.
	if len(ssh.calls) != 3 {
		t.Fatalf("ssh calls = %d, want 3", len(ssh.calls))
	}
	cmd := ssh.calls[1].command
	if containsSubstring(cmd, " -g") {
		t.Errorf("SSH command must NOT contain -g flag for cross-deploy, got: %s", cmd)
	}
}

func TestDeploy_TargetOnly_NoSSH(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "app"},
		})
	authInfo := testAuthInfo()

	// sshDeployer=nil + targetService="app" → ErrNotImplemented.
	_, err := DeploySSH(context.Background(), mock, "proj-1", nil, authInfo,
		"", "app", "", "", "")
	if err == nil {
		t.Fatal("expected error for nil SSH deployer with target-only")
	}

	var pe *platform.PlatformError
	if !errorAs(err, &pe) {
		t.Fatalf("expected PlatformError, got %T: %v", err, err)
	}
	if pe.Code != platform.ErrNotImplemented {
		t.Errorf("code = %s, want %s", pe.Code, platform.ErrNotImplemented)
	}
}

func TestDeploy_WorkingDir_MountPath_Rejected(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		workingDir string
		wantErr    bool
	}{
		{
			name:       "mount-style path rejected",
			workingDir: "/var/www/somehostname",
			wantErr:    true,
		},
		{
			name:       "nested mount path rejected",
			workingDir: "/var/www/appdev/subdir",
			wantErr:    true,
		},
		{
			name:       "default path accepted",
			workingDir: "/var/www",
			wantErr:    false,
		},
		{
			name:       "empty defaults to /var/www",
			workingDir: "",
			wantErr:    false,
		},
		{
			name:       "custom non-mount path accepted",
			workingDir: "/opt/app",
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mock := platform.NewMock().
				WithServices([]platform.ServiceStack{
					{ID: "svc-1", Name: "app"},
				})
			ssh := &mockSSHDeployer{output: []byte("ok")}
			authInfo := testAuthInfo()

			_, err := DeploySSH(context.Background(), mock, "proj-1", ssh, authInfo,
				"app", "app", "", tt.workingDir, "")

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error for mount-style workingDir")
				}
				var pe *platform.PlatformError
				if !errorAs(err, &pe) {
					t.Fatalf("expected PlatformError, got %T: %v", err, err)
				}
				if pe.Code != platform.ErrInvalidParameter {
					t.Errorf("code = %s, want %s", pe.Code, platform.ErrInvalidParameter)
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestDeploy_NilSSHDeployer(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "builder"},
			{ID: "svc-2", Name: "app"},
		})
	authInfo := testAuthInfo()

	_, err := DeploySSH(context.Background(), mock, "proj-1", nil, authInfo,
		"builder", "app", "", "", "")
	if err == nil {
		t.Fatal("expected error for nil SSH deployer")
	}

	var pe *platform.PlatformError
	if !errorAs(err, &pe) {
		t.Fatalf("expected PlatformError, got %T: %v", err, err)
	}
	if pe.Code != platform.ErrNotImplemented {
		t.Errorf("code = %s, want %s", pe.Code, platform.ErrNotImplemented)
	}
}

// TestDeploySSH_CrossDeployDirty_WarnsNotReproducible pins the GF-5
// visibility rule (docs/spec-workflows.md §4.9, §12.6): a working-tree
// (no explicit sha) cross-deploy whose source has uncommitted changes on
// top of HEAD must say so in Warnings — the target now runs code that
// isn't reproducible from git alone.
func TestDeploySSH_CrossDeployDirty_WarnsNotReproducible(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "builder"},
			{ID: "svc-2", Name: "app"},
		})
	sha := "fullhead1234567"
	ssh := &mockSSHDeployer{results: []sshResult{
		{output: []byte(sha + "\nM")},     // HeadStatus: dirty (non-empty porcelain char)
		{output: []byte(" M index.js\n")}, // the uncommitted paths
		{output: []byte("ok")},            // login+push
	}}
	authInfo := testAuthInfo()

	result, err := DeploySSH(context.Background(), mock, "proj-1", ssh, authInfo,
		"builder", "app", "", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Dirty {
		t.Fatalf("result.Dirty = false, want true")
	}
	want := fmt.Sprintf(
		"app received HEAD %s plus uncommitted changes from builder — not reproducible from git. Commit on builder and redeploy, or pass sha=%q to ship an exact commit. Uncommitted in /var/www on builder: index.js.",
		sha[:7], sha,
	)
	found := false
	for _, w := range result.Warnings {
		if w == want {
			found = true
		}
	}
	if !found {
		t.Errorf("Warnings = %#v, want to contain %q", result.Warnings, want)
	}
}

// TestDeploySSH_CrossDeployDirty_WarningNamesTheUncommittedFilesAndTheDirectory:
// the dirty warning says what is uncommitted and in which directory it was
// read, so an agent whose own `git status` ran elsewhere — the deploy copy
// beside a code volume — sees which tree shipped and what to commit there.
func TestDeploySSH_CrossDeployDirty_WarningNamesTheUncommittedFilesAndTheDirectory(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	repo := filepath.Join(root, "dify")
	env := isolatedGitEnv(t)
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	mustRunGit(t, repo, env, "init", "-q")
	if err := os.WriteFile(filepath.Join(repo, "app.js"), []byte("v1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mustRunGit(t, repo, env, "add", "-A")
	mustRunGit(t, repo, env, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-qm", "v1")
	for name, body := range map[string]string{"app.js": "v2\n", "footer.js": "new\n"} {
		if err := os.WriteFile(filepath.Join(repo, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	runner := git.SSHRunner{Executor: localShell{root: root}, Hostname: "apidev"}
	head, err := readWorkingTreeHead(t.Context(), runner, "/var/www/dify", DeployClassCross, "apidev", "webstage")
	if err != nil {
		t.Fatal(err)
	}
	if !head.dirty || len(head.warnings) != 1 {
		t.Fatalf("dirty = %v, warnings = %#v, want one dirty warning", head.dirty, head.warnings)
	}
	if want := "Uncommitted in /var/www/dify on apidev: app.js, footer.js."; !strings.HasSuffix(head.warnings[0], want) {
		t.Errorf("warning = %q, want it to end %q", head.warnings[0], want)
	}
}

// TestDeploySSH_SelfDeployDirty_NoReproducibilityWarning pins the negative
// case: a dirty SELF-deploy never carries the cross-deploy reproducibility
// warning — DM-7's repoState/notCarried facts already cover that ground,
// and the dev loop is dirty by design.
func TestDeploySSH_SelfDeployDirty_NoReproducibilityWarning(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "app"},
		})
	ssh := &mockSSHDeployer{results: []sshResult{
		{output: []byte("ZCP:GITFILE:0\nZCP:SUBMODULES:0\nZCP:HASREPO:1\nZCP:SHA:fullhead1234567\nZCP:DIRTY:1\nZCP:REPOSTATE:dirty\n")}, // self-deploy preflight: dirty
		{output: []byte("ok")}, // login+push
	}}
	authInfo := testAuthInfo()

	result, err := DeploySSH(context.Background(), mock, "proj-1", ssh, authInfo,
		"app", "app", "", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Dirty {
		t.Fatalf("result.Dirty = false, want true (fixture requires it for this pin to mean anything)")
	}
	for _, w := range result.Warnings {
		if containsSubstring(w, "not reproducible from git") {
			t.Errorf("self-deploy must not carry the cross-deploy reproducibility warning, got: %q", w)
		}
	}
}

// TestDeploySSH_ShaPath_NoReproducibilityWarning pins the other negative
// case: an explicit-sha deploy is never Dirty (it ships exactly sha's
// tree), so it never carries the reproducibility warning either.
func TestDeploySSH_ShaPath_NoReproducibilityWarning(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "builder"},
			{ID: "svc-2", Name: "app"},
		})
	ssh := &mockSSHDeployer{results: []sshResult{
		{output: []byte("f0115ba1234567\n")},     // resolve
		{output: []byte(validCommitZeropsYaml)},  // git show <sha>:zerops.yaml
		{output: []byte("/tmp/zcp-extract-1\n")}, // mktemp -d
		{output: []byte("")},                     // extract
		{output: []byte("ok")},                   // login+push
		{output: []byte("")},                     // cleanup
	}}
	authInfo := testAuthInfo()

	result, err := DeploySSH(context.Background(), mock, "proj-1", ssh, authInfo,
		"builder", "app", "", "", "abc123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Dirty {
		t.Fatalf("result.Dirty = true, want false — a deploy-from-commit is never dirty")
	}
	for _, w := range result.Warnings {
		if containsSubstring(w, "not reproducible from git") {
			t.Errorf("an explicit-sha deploy must not carry the reproducibility warning, got: %q", w)
		}
	}
}

// TestBuildSSHCommand_RunsTheCrewSaveBeforeThePush: a Mate with a crew keeps
// its copies' work through a deploy of their service — its save runs before
// zcli pushes, and a failing save pushes nothing; without a save the push
// runs as before.
func TestBuildSSHCommand_RunsTheCrewSaveBeforeThePush(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping under -short; needs real git binary")
	}
	tests := []struct {
		name   string
		save   string // the hook's body; "" writes none
		pushed bool
		saved  bool
	}{
		{name: "no crew, no save: the push runs", save: "", pushed: true},
		{name: "the save runs first", save: "touch saved\n", pushed: true, saved: true},
		{name: "a failing save pushes nothing", save: "touch saved\nexit 1\n", pushed: false, saved: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			bin := t.TempDir()
			env := isolatedGitEnv(t)
			mustRunGit(t, dir, env, "init", "-q", "-b", "main")
			mustRunGit(t, dir, env, "-c", "user.email=a@b", "-c", "user.name=a", "commit", "-q", "--allow-empty", "-m", "seed")
			if tt.save != "" {
				if err := os.WriteFile(filepath.Join(dir, ".git", "t3-crew-save"), []byte("#!/bin/sh\ncd \"$(dirname \"$0\")/..\" || exit 1\n"+tt.save), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			// zcli records whether the save had run when the push started.
			fake := "#!/bin/sh\nif [ -e saved ]; then echo after-save > pushed; else echo pushed > pushed; fi\n"
			if err := os.WriteFile(filepath.Join(bin, "zcli"), []byte(fake), 0o755); err != nil {
				t.Fatal(err)
			}
			env = append(env, "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			//nolint:gosec // test-only: buildSSHCommand over t.TempDir paths
			cmd := exec.CommandContext(ctx, "bash", "-c", buildSSHCommand(auth.Info{Token: "tok"}, "svc-1", dir, "", true, "", filepath.Join(t.TempDir(), "zcp-push-test")))
			cmd.Env = env
			_ = cmd.Run()
			pushed, _ := os.ReadFile(filepath.Join(dir, "pushed"))
			_, savedErr := os.Stat(filepath.Join(dir, "saved"))
			want := ""
			if tt.pushed {
				want = "pushed\n"
				if tt.saved {
					want = "after-save\n"
				}
			}
			if string(pushed) != want || (savedErr == nil) != tt.saved {
				t.Errorf("pushed=%q saved=%v, want pushed=%q saved=%v", pushed, savedErr == nil, want, tt.saved)
			}
		})
	}
}

// TestDeploySSH_FailingCrewSave_NamesTheSaveAndHoldsTheDeploy: a refused
// deploy says the Mate's crew save failed, quotes its stderr tail and that
// the person's own deploy is held so crewmates' unsaved work is not lost.
func TestDeploySSH_FailingCrewSave_NamesTheSaveAndHoldsTheDeploy(t *testing.T) {
	t.Parallel()
	pe := classifySSHError(&platform.SSHExecError{
		Hostname: "builder",
		Output:   "t3-crew-save: cannot write ref: disk full\n" + crewSaveFailedMarker + "\n",
		Err:      fmt.Errorf("exit status 1"),
	}, "builder", "app")
	for _, want := range []string{"crew save", "disk full", "held", "unsaved work"} {
		if !containsSubstring(pe.Message, want) {
			t.Errorf("message should contain %q, got: %s", want, pe.Message)
		}
	}
	if containsSubstring(pe.Message, crewSaveFailedMarker) {
		t.Errorf("marker leaked into message: %s", pe.Message)
	}
}

// TestBuildSSHCommand_ReturnsOnUpload_BuildWaitReportsTheBuild: the push
// command returns once zcli has uploaded the package — zcli still running on
// the build — and the build wait then reports zcli's own output and exit
// status, a failure before the upload included.
func TestBuildSSHCommand_ReturnsOnUpload_BuildWaitReportsTheBuild(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping under -short; needs real git binary")
	}
	tests := []struct {
		name      string
		zcli      string // the fake zcli's body; it may wait for ./build-go
		running   bool   // zcli still runs when the push command returns
		wantOut   string
		wantCode  int
		pushedOut string // what the push command's own output carries
	}{
		{
			name:      "upload done, the build fails later",
			zcli:      "echo 'Package uploaded'\necho 'Deploying service'\nwhile [ ! -e build-go ]; do sleep 0.1; done\necho 'BUILD FAILED: out of memory'\nexit 3\n",
			running:   true,
			wantOut:   "BUILD FAILED: out of memory",
			wantCode:  3,
			pushedOut: "Deploying service",
		},
		{
			name:     "upload fails",
			zcli:     "echo 'Package upload failed' >&2\nexit 2\n",
			wantOut:  "Package upload failed",
			wantCode: 2,
		},
		{
			name:     "build passes",
			zcli:     "echo 'Deploying service'\nwhile [ ! -e build-go ]; do sleep 0.1; done\necho 'BUILD ARTEFACTS READY TO DEPLOY'\n",
			running:  true,
			wantOut:  "BUILD ARTEFACTS READY TO DEPLOY",
			wantCode: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			bin := t.TempDir()
			pushDir := filepath.Join(t.TempDir(), "zcp-push-test")
			env := isolatedGitEnv(t)
			mustRunGit(t, dir, env, "init", "-q", "-b", "main")
			mustRunGit(t, dir, env, "-c", "user.email=a@b", "-c", "user.name=a", "commit", "-q", "--allow-empty", "-m", "seed")
			if err := os.WriteFile(filepath.Join(bin, "zcli"), []byte("#!/bin/sh\n"+tt.zcli), 0o755); err != nil {
				t.Fatal(err)
			}
			env = append(env, "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			run := func(script string) (string, int) {
				cmd := exec.CommandContext(ctx, "bash", "-c", script)
				cmd.Env = env
				out, err := cmd.CombinedOutput()
				code := 0
				var exitErr *exec.ExitError
				if errors.As(err, &exitErr) {
					code = exitErr.ExitCode()
				} else if err != nil {
					t.Fatalf("run: %v", err)
				}
				return string(out), code
			}

			pushOut, pushCode := run(buildSSHCommand(auth.Info{Token: "tok"}, "svc-1", dir, "", false, "", pushDir))
			if pushCode != 0 {
				t.Fatalf("push command exit %d, want 0: %s", pushCode, pushOut)
			}
			if !strings.Contains(pushOut, tt.pushedOut) {
				t.Errorf("push output %q, want it to carry %q", pushOut, tt.pushedOut)
			}
			if _, err := os.Stat(filepath.Join(pushDir, "rc")); (err != nil) != tt.running {
				t.Errorf("zcli still running when the push returned = %v, want %v", err != nil, tt.running)
			}
			if err := os.WriteFile(filepath.Join(dir, "build-go"), nil, 0o600); err != nil {
				t.Fatal(err)
			}
			waitOut, waitCode := run(buildWaitCommand(pushDir))
			if waitCode != tt.wantCode || !strings.Contains(waitOut, tt.wantOut) {
				t.Errorf("build wait = exit %d %q, want exit %d carrying %q", waitCode, waitOut, tt.wantCode, tt.wantOut)
			}
			if _, err := os.Stat(pushDir); !os.IsNotExist(err) {
				t.Errorf("push run directory left behind: %v", err)
			}
		})
	}
}

// pushRig is a source repository with a fake zcli on PATH, for running the
// push and build-wait commands in real bash.
type pushRig struct {
	t       *testing.T
	dir     string
	pushDir string
	env     []string
}

func newPushRig(t *testing.T, zcli string) *pushRig {
	t.Helper()
	dir := t.TempDir()
	bin := t.TempDir()
	env := isolatedGitEnv(t)
	mustRunGit(t, dir, env, "init", "-q", "-b", "main")
	mustRunGit(t, dir, env, "-c", "user.email=a@b", "-c", "user.name=a", "commit", "-q", "--allow-empty", "-m", "seed")
	if err := os.WriteFile(filepath.Join(bin, "zcli"), []byte("#!/bin/sh\n"+zcli), 0o755); err != nil {
		t.Fatal(err)
	}
	return &pushRig{
		t:       t,
		dir:     dir,
		pushDir: filepath.Join(t.TempDir(), "zcp-push-test"),
		env:     append(env, "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH")),
	}
}

// run runs script in bash until it exits or ctx ends, as an ssh session does.
func (r *pushRig) run(ctx context.Context, script string) (string, int) {
	r.t.Helper()
	cmd := exec.CommandContext(ctx, "bash", "-c", script)
	cmd.Env = r.env
	out, err := cmd.CombinedOutput()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return string(out), exitErr.ExitCode()
	}
	if err != nil {
		r.t.Fatalf("run: %v", err)
	}
	return string(out), 0
}

// TestBuildSSHCommand_PushStoppedBeforeItsUpload_DeploysNothing: a push whose
// session ends before zcli says it is deploying — the call cancelled or past
// its ceiling — is stopped by the next call: zcli never asks for the build
// and its run directory is gone. A push already past that line is left to
// its build, and the stop's output says the build started.
func TestBuildSSHCommand_PushStoppedBeforeItsUpload_DeploysNothing(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping under -short; needs real git binary")
	}
	tests := []struct {
		name       string
		zcli       string
		wantBuilt  bool
		wantMarker bool
	}{
		{
			name:      "stopped mid-upload",
			zcli:      "echo 'Uploading'\nwhile [ ! -e upload-go ]; do sleep 0.1; done\necho 'Deploying service'\necho built > built\n",
			wantBuilt: false,
		},
		{
			name:       "stopped after the upload",
			zcli:       "echo 'Deploying service'\necho built > built\nwhile [ ! -e upload-go ]; do sleep 0.1; done\n",
			wantBuilt:  true,
			wantMarker: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := newPushRig(t, tt.zcli)
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			// The upload call's session ends as a stopped ssh client's does.
			session, endSession := context.WithTimeout(ctx, 2*time.Second)
			defer endSession()
			r.run(session, buildSSHCommand(auth.Info{Token: "tok"}, "svc-1", r.dir, "", false, "", r.pushDir))

			stopOut, _ := r.run(ctx, pushStopCommand(r.pushDir))
			if err := os.WriteFile(filepath.Join(r.dir, "upload-go"), nil, 0o600); err != nil {
				t.Fatal(err)
			}
			time.Sleep(time.Second)
			if _, err := os.Stat(filepath.Join(r.dir, "built")); (err == nil) != tt.wantBuilt {
				t.Errorf("zcli asked for the build = %v, want %v", err == nil, tt.wantBuilt)
			}
			if got := isSSHBuildTriggered(stopOut); got != tt.wantMarker {
				t.Errorf("stop output says the build started = %v, want %v: %q", got, tt.wantMarker, stopOut)
			}
			if _, err := os.Stat(r.pushDir); !tt.wantMarker && !os.IsNotExist(err) {
				t.Errorf("stopped push left its run directory: %v", err)
			}
			if tt.wantMarker {
				r.run(ctx, buildWaitCommand(r.pushDir))
			}
		})
	}
}

// TestBuildWaitCommand_RunDirectoryAbsent_EndsAtOnce: a build wait that
// reaches a container without the push's run directory — another container
// of the source, or a replaced one — ends at once with exit 254 rather than
// waiting out its ceiling.
func TestBuildWaitCommand_RunDirectoryAbsent_EndsAtOnce(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping under -short; needs real git binary")
	}
	r := newPushRig(t, "exit 0\n")
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	out, code := r.run(ctx, buildWaitCommand(r.pushDir))
	if code != 254 {
		t.Errorf("build wait without its run directory = exit %d %q, want exit 254 at once", code, out)
	}
}

// TestBuildSSHCommand_GitSetupFails_ReportsItsOwnError: a push whose git
// setup fails ends with that failure's own line and status, not a line about
// zcp's run directory.
func TestBuildSSHCommand_GitSetupFails_ReportsItsOwnError(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping under -short; needs real git binary")
	}
	r := newPushRig(t, "exit 0\n")
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	missing := filepath.Join(r.dir, "missing")
	out, code := r.run(ctx, buildSSHCommand(auth.Info{Token: "tok"}, "svc-1", missing, "", false, "", r.pushDir))
	last := lastLines(out, 1)
	if code == 0 || !strings.Contains(last, missing) || strings.Contains(out, "zcp-push") {
		t.Errorf("failed git setup = exit %d, last line %q, want its own failure as the last line:\n%s", code, last, out)
	}
}

// TestBuildSSHCommand_NextPush_RemovesOldFinishedRuns: a push removes the run
// directories of pushes that finished over an hour ago, which no build wait
// will read any more, and keeps recent and still-running ones.
func TestBuildSSHCommand_NextPush_RemovesOldFinishedRuns(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping under -short; needs real git binary")
	}
	r := newPushRig(t, "echo 'Deploying service'\n")
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	runs := filepath.Dir(r.pushDir)
	old := time.Now().Add(-2 * time.Hour)
	for _, run := range []struct {
		name     string
		finished time.Time // zero: still running
		started  time.Time
	}{
		{name: "zcp-push-OLDDONE", finished: old, started: old},
		{name: "zcp-push-NEWDONE", finished: time.Now(), started: old},
		{name: "zcp-push-OLDRUNNING", started: old},
	} {
		d := filepath.Join(runs, run.name)
		if err := os.Mkdir(d, 0o700); err != nil {
			t.Fatal(err)
		}
		if !run.finished.IsZero() {
			rc := filepath.Join(d, "rc")
			if err := os.WriteFile(rc, []byte("0\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chtimes(rc, run.finished, run.finished); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.Chtimes(d, run.started, run.started); err != nil {
			t.Fatal(err)
		}
	}
	if out, code := r.run(ctx, buildSSHCommand(auth.Info{Token: "tok"}, "svc-1", r.dir, "", false, "", r.pushDir)); code != 0 {
		t.Fatalf("push = exit %d: %s", code, out)
	}
	r.run(ctx, buildWaitCommand(r.pushDir))
	for name, wantKept := range map[string]bool{"zcp-push-OLDDONE": false, "zcp-push-NEWDONE": true, "zcp-push-OLDRUNNING": true} {
		if _, err := os.Stat(filepath.Join(runs, name)); (err == nil) != wantKept {
			t.Errorf("%s kept = %v, want %v", name, err == nil, wantKept)
		}
	}
}

// stoppedPushSSH fails the push's upload call as a stopped ssh client does:
// its context cancelled, or its ceiling hit. Every call records whether its
// context was still live.
type stoppedPushSSH struct {
	mockSSHDeployer
	stop    error
	cancel  context.CancelFunc
	ctxErrs []error
}

func (m *stoppedPushSSH) ExecSSH(ctx context.Context, hostname, command string) ([]byte, error) {
	m.ctxErrs = append(m.ctxErrs, ctx.Err())
	if !strings.Contains(command, "zcli push") {
		return m.mockSSHDeployer.ExecSSH(ctx, hostname, command)
	}
	m.calls = append(m.calls, sshCall{hostname: hostname, command: command})
	m.cancel()
	return []byte("Uploading\n"), &platform.SSHExecError{Hostname: hostname, Output: "Uploading\n", Err: m.stop}
}

// TestDeploySSH_PushStoppedBeforeItsUpload_StopsZcliAndSaysNoBuild: a deploy
// whose push is cancelled or times out before zcli says it is deploying
// stops zcli over a call of its own, which its cancellation does not stop,
// and says the push started no build — never that the source ran out of
// memory.
func TestDeploySSH_PushStoppedBeforeItsUpload_StopsZcliAndSaysNoBuild(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		stop     error
		cancel   bool
		wantWord string
	}{
		{name: "cancelled", stop: fmt.Errorf("ssh stopped: %w (signal: killed)", context.Canceled), cancel: true, wantWord: "cancelled"},
		{name: "past the ceiling", stop: fmt.Errorf("ssh stopped: %w (signal: killed)", context.DeadlineExceeded), wantWord: "timed out"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mock := platform.NewMock().WithServices([]platform.ServiceStack{
				{ID: "svc-1", Name: "builder"},
				{ID: "svc-2", Name: "app"},
			})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			ssh := &stoppedPushSSH{stop: tt.stop, cancel: func() {}}
			if tt.cancel {
				ssh.cancel = cancel
			}
			_, err := DeploySSH(ctx, mock, "proj-1", ssh, testAuthInfo(), "builder", "app", "", "", "")
			var pe *platform.PlatformError
			if !errorAs(err, &pe) {
				t.Fatalf("want a PlatformError, got %T: %v", err, err)
			}
			if !strings.Contains(pe.Message, tt.wantWord) || !strings.Contains(pe.Message, "started no build") ||
				strings.Contains(pe.Message, "OOM") || strings.Contains(pe.Suggestion, "zerops_scale") {
				t.Errorf("message %q / suggestion %q, want it %s and to have started no build, without OOM or scaling", pe.Message, pe.Suggestion, tt.wantWord)
			}
			var push string
			for _, c := range ssh.calls {
				if strings.Contains(c.command, "zcli push") {
					push = c.command
				}
			}
			pushDir := regexp.MustCompile(`/tmp/zcp-push-[A-Z2-7]+`).FindString(push)
			last := ssh.calls[len(ssh.calls)-1]
			if pushDir == "" || last.command != pushStopCommand(pushDir) {
				t.Fatalf("last call = %q, want the stop of push run %q", last.command, pushDir)
			}
			if ctxErr := ssh.ctxErrs[len(ssh.ctxErrs)-1]; ctxErr != nil {
				t.Errorf("stop call ran with a dead context: %v", ctxErr)
			}
		})
	}
}
