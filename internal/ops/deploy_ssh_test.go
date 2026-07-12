// Tests for: ops/deploy.go — Deploy with SSH mode (SSH-only, no local deploy).
package ops

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/auth"
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
}

func (m *mockSSHDeployer) ExecSSH(_ context.Context, hostname, command string) ([]byte, error) {
	m.calls = append(m.calls, sshCall{hostname: hostname, command: command})
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
				tt.sourceService, tt.targetService, "", tt.workingDir)
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
			if len(ssh.calls) != 1 {
				t.Fatalf("ssh calls = %d, want 1", len(ssh.calls))
			}
			if ssh.calls[0].hostname != "builder" {
				t.Errorf("ssh hostname = %s, want builder", ssh.calls[0].hostname)
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
		"nonexistent", "app", "", "")
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
		"builder", "nonexistent", "", "")
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
		"weatherbun", "weatherbun", "", "/var/www/weatherbun")
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
		"builder", "app", "", "")
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
		"builder", "app", "", "")
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
		"builder", "app", "", "")
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
		"builder", "app", "", "")
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
		"builder", "app", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Mode != "ssh" {
		t.Errorf("mode = %s, want ssh", result.Mode)
	}
	// Verify login command is present without --zeropsRegion.
	if len(ssh.calls) != 1 {
		t.Fatalf("ssh calls = %d, want 1", len(ssh.calls))
	}
	cmd := ssh.calls[0].command
	if !containsSubstring(cmd, "zcli login -- 'test-token'") {
		t.Errorf("SSH command should contain 'zcli login -- test-token', got: %s", cmd)
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
				"builder", "app", "", "")
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
				"builder", "app", "", "")
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
	cmd := buildSSHCommand(authInfo, "svc-target", "/var/www", "", false)

	wantContains := []string{
		"zcli login -- 'test-token'",
		"cd '/var/www'",
		"(test -d .git || git init -q -b main)",
		`(test -n "$(git config user.email)" || git config user.email 'agent@zerops.io') && (test -n "$(git config user.name)" || git config user.name 'Zerops Agent')`,
		`git rev-parse -q --verify HEAD >/dev/null || git update-ref HEAD`,
		"commit-tree",
		"-m 'zcp init'",
		"zcli push --service-id svc-target",
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

// extractGitEnsureChain pulls the self-heal chain (cd ... init ...
// identity ... HEAD guarantee) out of buildSSHCommand's full output —
// everything up to (not including) " && zcli push". Running only this
// piece against a scratch dir is what a cold-path deploy would actually
// execute; the trailing `zcli push` would fail locally against a fake
// token and isn't the part these tests care about.
func extractGitEnsureChain(t *testing.T, dir string) string {
	t.Helper()
	authInfo := auth.Info{Token: "tok"}
	full := buildSSHCommand(authInfo, "svc-target", dir, "", false)
	chain, _, found := strings.Cut(full, " && zcli push")
	if !found {
		t.Fatalf("command missing `zcli push` anchor, shape drifted:\n%s", full)
	}
	if prefix := "zcli login -- 'tok' && "; strings.HasPrefix(chain, prefix) {
		chain = chain[len(prefix):]
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
		"", "app", "", "")
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
	if len(ssh.calls) != 1 {
		t.Fatalf("ssh calls = %d, want 1", len(ssh.calls))
	}
	if ssh.calls[0].hostname != "app" {
		t.Errorf("ssh hostname = %s, want app", ssh.calls[0].hostname)
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
		"app", "app", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Mode != "ssh" {
		t.Errorf("mode = %s, want ssh", result.Mode)
	}
	if len(ssh.calls) != 1 {
		t.Fatalf("ssh calls = %d, want 1", len(ssh.calls))
	}
	cmd := ssh.calls[0].command
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
		"builder", "app", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ssh.calls) != 1 {
		t.Fatalf("ssh calls = %d, want 1", len(ssh.calls))
	}
	cmd := ssh.calls[0].command
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
		"", "app", "", "")
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
				"app", "app", "", tt.workingDir)

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
		"builder", "app", "", "")
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
