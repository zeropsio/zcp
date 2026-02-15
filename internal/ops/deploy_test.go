// Tests for: ops/deploy.go — Deploy with SSH and local modes.
package ops

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/platform"
)

type sshCall struct {
	hostname string
	command  string
}

type mockSSHDeployer struct {
	output []byte
	err    error
	calls  []sshCall
}

func (m *mockSSHDeployer) ExecSSH(_ context.Context, hostname, command string) ([]byte, error) {
	m.calls = append(m.calls, sshCall{hostname: hostname, command: command})
	return m.output, m.err
}

type zcliCall struct {
	args []string
}

type mockLocalDeployer struct {
	output []byte
	err    error
	calls  []zcliCall
}

func (m *mockLocalDeployer) ExecZcli(_ context.Context, args ...string) ([]byte, error) {
	m.calls = append(m.calls, zcliCall{args: args})
	return m.output, m.err
}

func testAuthInfo() auth.Info {
	return auth.Info{
		Token:   "test-token",
		APIHost: "api.app-prg1.zerops.io",
		Region:  "prg1",
	}
}

func TestDeploy_SSHMode_Success(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		sourceService string
		targetService string
		setup         string
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
			name:          "ssh with setup",
			sourceService: "builder",
			targetService: "app",
			setup:         "npm install",
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
			local := &mockLocalDeployer{}
			authInfo := testAuthInfo()

			result, err := Deploy(context.Background(), mock, "proj-1", ssh, local, authInfo,
				tt.sourceService, tt.targetService, tt.setup, tt.workingDir)
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
	local := &mockLocalDeployer{}
	authInfo := testAuthInfo()

	_, err := Deploy(context.Background(), mock, "proj-1", ssh, local, authInfo,
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
	local := &mockLocalDeployer{}
	authInfo := testAuthInfo()

	_, err := Deploy(context.Background(), mock, "proj-1", ssh, local, authInfo,
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
	local := &mockLocalDeployer{}
	authInfo := testAuthInfo()

	_, err := Deploy(context.Background(), mock, "proj-1", ssh, local, authInfo,
		"builder", "app", "", "")
	if err == nil {
		t.Fatal("expected error for SSH failure")
	}
}

func TestDeploy_LocalMode_Success(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "app"},
		})
	ssh := &mockSSHDeployer{}
	local := &mockLocalDeployer{output: []byte("deployed")}
	authInfo := testAuthInfo()

	result, err := Deploy(context.Background(), mock, "proj-1", ssh, local, authInfo,
		"", "app", "", "/tmp/build")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "BUILD_TRIGGERED" {
		t.Errorf("status = %s, want BUILD_TRIGGERED", result.Status)
	}
	if result.MonitorHint == "" {
		t.Error("monitorHint should not be empty")
	}
	if result.Mode != "local" {
		t.Errorf("mode = %s, want local", result.Mode)
	}
	if result.TargetService != "app" {
		t.Errorf("targetService = %s, want app", result.TargetService)
	}
	if len(local.calls) != 1 {
		t.Fatalf("zcli calls = %d, want 1", len(local.calls))
	}
}

func TestDeploy_LocalMode_TargetNotFound(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{})
	ssh := &mockSSHDeployer{}
	local := &mockLocalDeployer{}
	authInfo := testAuthInfo()

	_, err := Deploy(context.Background(), mock, "proj-1", ssh, local, authInfo,
		"", "nonexistent", "", "")
	if err == nil {
		t.Fatal("expected error for nonexistent target service")
	}
}

func TestDeploy_NoParams(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock()
	ssh := &mockSSHDeployer{}
	local := &mockLocalDeployer{}
	authInfo := testAuthInfo()

	_, err := Deploy(context.Background(), mock, "proj-1", ssh, local, authInfo,
		"", "", "", "")
	if err == nil {
		t.Fatal("expected error for no params")
	}

	var pe *platform.PlatformError
	if !errorAs(err, &pe) {
		t.Fatalf("expected PlatformError, got %T: %v", err, err)
	}
	if pe.Code != platform.ErrInvalidParameter {
		t.Errorf("code = %s, want %s", pe.Code, platform.ErrInvalidParameter)
	}
}

func TestDeploy_ModeDetection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		sourceService string
		targetService string
		wantMode      string
	}{
		{
			name:          "source + target = SSH",
			sourceService: "builder",
			targetService: "app",
			wantMode:      "ssh",
		},
		{
			name:          "only target = local",
			sourceService: "",
			targetService: "app",
			wantMode:      "local",
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
			local := &mockLocalDeployer{output: []byte("ok")}
			authInfo := testAuthInfo()

			result, err := Deploy(context.Background(), mock, "proj-1", ssh, local, authInfo,
				tt.sourceService, tt.targetService, "", "")
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

	_, err := Deploy(context.Background(), mock, "proj-1", nil, nil, authInfo,
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

func TestDeploy_NilLocalDeployer(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "app"},
		})
	authInfo := testAuthInfo()

	_, err := Deploy(context.Background(), mock, "proj-1", nil, nil, authInfo,
		"", "app", "", "")
	if err == nil {
		t.Fatal("expected error for nil local deployer")
	}

	var pe *platform.PlatformError
	if !errorAs(err, &pe) {
		t.Fatalf("expected PlatformError, got %T: %v", err, err)
	}
	if pe.Code != platform.ErrNotImplemented {
		t.Errorf("code = %s, want %s", pe.Code, platform.ErrNotImplemented)
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
	local := &mockLocalDeployer{}
	authInfo := auth.Info{
		Token:   "test-token",
		APIHost: "api.app-fra1.zerops.io",
		Region:  "fra1",
	}

	result, err := Deploy(context.Background(), mock, "proj-1", ssh, local, authInfo,
		"builder", "app", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Mode != "ssh" {
		t.Errorf("mode = %s, want ssh", result.Mode)
	}
	// Verify region appears in the SSH command.
	if len(ssh.calls) != 1 {
		t.Fatalf("ssh calls = %d, want 1", len(ssh.calls))
	}
	cmd := ssh.calls[0].command
	if !containsSubstring(cmd, "fra1") {
		t.Errorf("SSH command should contain region 'fra1', got: %s", cmd)
	}
}

func TestBuildSSHCommand_GitGuard(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		authInfo  auth.Info
		serviceID string
		setup     string
		workDir   string
		wantParts []string
	}{
		{
			name:      "basic command contains git guard",
			authInfo:  testAuthInfo(),
			serviceID: "svc-123",
			workDir:   "/var/www",
			wantParts: []string{
				"test -d .git",
				"git init -q",
				"git add -A",
				"git commit -q -m 'deploy'",
				"(test -d .git || (git init -q",
				"zcli push --serviceId svc-123",
			},
		},
		{
			name:      "with setup command",
			authInfo:  testAuthInfo(),
			serviceID: "svc-456",
			setup:     "npm install",
			workDir:   "/opt/app",
			wantParts: []string{
				"test -d .git",
				"git init -q",
				"npm install",
				"zcli push --serviceId svc-456",
			},
		},
		{
			name: "with different region",
			authInfo: auth.Info{
				Token:   "my-token",
				APIHost: "api.app-fra1.zerops.io",
				Region:  "fra1",
			},
			serviceID: "svc-789",
			workDir:   "/var/www",
			wantParts: []string{
				"test -d .git",
				"fra1",
				"zcli push --serviceId svc-789",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cmd := buildSSHCommand(tt.authInfo, tt.serviceID, tt.setup, tt.workDir)

			for _, part := range tt.wantParts {
				if !contains(cmd, part) {
					t.Errorf("command missing %q\ngot: %s", part, cmd)
				}
			}
		})
	}
}

func containsSubstring(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && contains(s, sub))
}

func contains(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestPrepareGitRepo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		setupDir   func(t *testing.T, dir string)
		wantGitDir bool
		wantCommit bool
		wantNoOp   bool // existing repo should not be modified
	}{
		{
			name: "dir without .git creates repo with commit",
			setupDir: func(t *testing.T, dir string) {
				t.Helper()
				// Create a file so git add -A has something to commit.
				if err := os.WriteFile(filepath.Join(dir, "index.ts"), []byte("console.log('hi')"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			wantGitDir: true,
			wantCommit: true,
		},
		{
			name: "dir with existing .git is no-op",
			setupDir: func(t *testing.T, dir string) {
				t.Helper()
				// Initialize a real git repo with a commit.
				runGit(t, dir, "init", "-q")
				runGit(t, dir, "config", "user.email", "test@test.com")
				runGit(t, dir, "config", "user.name", "test")
				if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0o644); err != nil {
					t.Fatal(err)
				}
				runGit(t, dir, "add", "-A")
				runGit(t, dir, "commit", "-q", "-m", "initial")
			},
			wantGitDir: true,
			wantCommit: true,
			wantNoOp:   true,
		},
		{
			name: "empty dir creates repo with allow-empty commit",
			setupDir: func(t *testing.T, _ string) {
				t.Helper()
				// No files — prepareGitRepo should use --allow-empty.
			},
			wantGitDir: true,
			wantCommit: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			tt.setupDir(t, dir)

			// Capture HEAD before if we expect no-op.
			var headBefore string
			if tt.wantNoOp {
				headBefore = gitHead(t, dir)
			}

			err := prepareGitRepo(context.Background(), dir)
			if err != nil {
				t.Fatalf("prepareGitRepo() error: %v", err)
			}

			// Check .git exists.
			if tt.wantGitDir {
				info, err := os.Stat(filepath.Join(dir, ".git"))
				if err != nil {
					t.Fatalf(".git directory should exist: %v", err)
				}
				if !info.IsDir() {
					t.Fatal(".git should be a directory")
				}
			}

			// Check commit exists.
			if tt.wantCommit {
				head := gitHead(t, dir)
				if head == "" {
					t.Fatal("expected at least one commit, got none")
				}
			}

			// No-op: HEAD should be unchanged.
			if tt.wantNoOp {
				headAfter := gitHead(t, dir)
				if headBefore != headAfter {
					t.Errorf("HEAD changed: before=%s, after=%s", headBefore, headAfter)
				}
			}
		})
	}
}

// runGit executes a git command in the given directory.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", args...) //nolint:gosec // test helper with static args
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}

// gitHead returns the HEAD commit hash, or empty string if no commits.
func gitHead(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", "rev-parse", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return string(out[:len(out)-1]) // trim newline
}
