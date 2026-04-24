// Tests for: ops/deploy.go — Deploy with SSH mode (SSH-only, no local deploy).
package ops

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
// atomic safety-net, no top-level `git config user.*`, identity + init
// paired inside the OR branch, followed by commit + push. Identity lands
// from the DeployGitIdentity package constant — not from a caller — so
// shell escaping of user-controlled strings no longer applies (the
// attack surface vanished when the GitIdentity parameter was removed).
// shellQuote itself is exercised by TestShellQuote below.
func TestBuildSSHCommand_Shape(t *testing.T) {
	t.Parallel()

	authInfo := auth.Info{
		Token:   "test-token",
		APIHost: "api.app-prg1.zerops.io",
		Region:  "prg1",
	}
	cmd := buildSSHCommand(authInfo, "svc-target", "/var/www", "", false)

	// Must contain: login, atomic safety-net, commit, push.
	wantContains := []string{
		"zcli login -- 'test-token'",
		"cd /var/www",
		"(test -d .git || (git init -q -b main && git config user.email 'agent@zerops.io' && git config user.name 'Zerops Agent'))",
		"git add -A",
		"git commit -q -m 'deploy'",
		"zcli push --service-id svc-target",
	}
	for _, want := range wantContains {
		if !containsSubstring(cmd, want) {
			t.Errorf("missing substring %q in command:\n%s", want, cmd)
		}
	}

	// Must NOT contain: top-level (outside OR branch) git config. The
	// safety-net form keeps identity paired with init, so migration +
	// recovery paths pick up both atomically. A regression that splits
	// config out would re-introduce the "Please tell me who you are"
	// failure on services with no pre-existing .git/.
	//
	// Checking this by ensuring the only occurrence of the config
	// statements is inside the parenthesized OR branch — test by ruling
	// out the old top-level form entirely.
	forbidden := " && git config user.email " // note leading/trailing space — matches top-level position
	// After the OR branch, the `&& git config` appears inside parens, not
	// as a top-level conjunct. Easier check: count occurrences outside the
	// OR branch. Simplest conservative check: the pattern `) && git config
	// user.email` (identity right after the safety-net OR branch closes)
	// would mean config was split.
	if containsSubstring(cmd, ")) && git config user.email") {
		t.Errorf("git config lives outside the OR branch — regression toward split identity:\n%s", cmd)
	}
	_ = forbidden
}

// TestBuildSSHCommand_FreshInitPath executes the emitted command against
// a real git binary on a scratch dir without .git/, proving the atomic
// OR branch actually leaves a committed-ready repo behind (init + both
// config entries). This is the migration lock-in: if a future refactor
// splits config out of the OR branch, the following simulated "cold
// path" deploy will fail with "Please tell me who you are" on `git
// commit`, or user.email/user.name won't be set.
//
// Skipped under -short and when git is not on PATH.
func TestBuildSSHCommand_FreshInitPath(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping under -short; needs real git binary")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	t.Parallel()

	dir := t.TempDir()
	// Extract just the safety-net expression — not the whole command,
	// which tries to `zcli push` at the end. We want to prove the atomic
	// init+config piece behaves correctly in isolation.
	//
	// The safety-net is the second-through-fourth statement of the
	// full command; grab it via the same emitter we care about rather
	// than re-deriving the string.
	authInfo := auth.Info{Token: "tok"}
	full := buildSSHCommand(authInfo, "svc-target", dir, "", false)

	// The safety-net chain starts at `cd <dir>` and ends just before
	// `git add -A`. Slice that out; running it in a subshell inside dir
	// is what a cold-path deploy would actually do on the container.
	// Extract up to (but not including) ` && git add -A`.
	idx := strings.Index(full, " && git add -A")
	if idx < 0 {
		t.Fatalf("command missing `git add -A` anchor, shape drifted:\n%s", full)
	}
	// Drop the leading `zcli login -- 'tok' && ` prefix so we run only
	// the init+config piece. zcli login would fail locally — not the
	// part we care about here.
	full = full[:idx]
	if prefix := "zcli login -- 'tok' && "; strings.HasPrefix(full, prefix) {
		full = full[len(prefix):]
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, "bash", "-c", full).CombinedOutput()
	if err != nil {
		t.Fatalf("safety-net chain failed: %v\noutput: %s\ncommand: %s", err, out, full)
	}

	// Assert .git/ exists and identity is set.
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		t.Errorf(".git not created: %v", err)
	}
	want := map[string]string{
		"user.email": "agent@zerops.io",
		"user.name":  "Zerops Agent",
	}
	for key, wantVal := range want {
		got, gErr := exec.CommandContext(ctx, "git", "-C", dir, "config", "--get", key).Output()
		if gErr != nil {
			t.Errorf("git config --get %s: %v", key, gErr)
			continue
		}
		if strings.TrimSpace(string(got)) != wantVal {
			t.Errorf("git config %s: got %q, want %q", key, strings.TrimSpace(string(got)), wantVal)
		}
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
