// Tests for: ops/deploy.go — Deploy with SSH mode (SSH-only, no local deploy).
package ops

import (
	"context"
	"fmt"
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

			result, err := Deploy(context.Background(), mock, "proj-1", ssh, authInfo,
				tt.sourceService, tt.targetService, tt.workingDir, false)
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

	_, err := Deploy(context.Background(), mock, "proj-1", ssh, authInfo,
		"nonexistent", "app", "", false)
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

	_, err := Deploy(context.Background(), mock, "proj-1", ssh, authInfo,
		"builder", "nonexistent", "", false)
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

	_, err := Deploy(context.Background(), mock, "proj-1", ssh, authInfo,
		"builder", "app", "", false)
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

	_, err := Deploy(context.Background(), mock, "proj-1", ssh, authInfo,
		"builder", "app", "", false)
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

	_, err := Deploy(context.Background(), mock, "proj-1", ssh, authInfo,
		"builder", "app", "", false)
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

	_, err := Deploy(context.Background(), mock, "proj-1", ssh, authInfo,
		"builder", "app", "", false)
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

	result, err := Deploy(context.Background(), mock, "proj-1", ssh, authInfo,
		"builder", "app", "", false)
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
	if !containsSubstring(cmd, "zcli login test-token") {
		t.Errorf("SSH command should contain 'zcli login test-token', got: %s", cmd)
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

			result, err := Deploy(context.Background(), mock, "proj-1", ssh, authInfo,
				"builder", "app", "", false)
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

			_, err := Deploy(context.Background(), mock, "proj-1", ssh, authInfo,
				"builder", "app", "", false)
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

func TestBuildSSHCommand_ShellQuoting(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		email     string
		fullName  string
		checkFunc func(t *testing.T, cmd string)
	}{
		{
			name:     "command injection via dollar",
			email:    "test@example.com",
			fullName: "$(whoami)",
			checkFunc: func(t *testing.T, cmd string) {
				t.Helper()
				if !containsSubstring(cmd, "'$(whoami)'") {
					t.Errorf("expected $(whoami) to be inside single quotes, got: %s", cmd)
				}
			},
		},
		{
			name:     "backtick injection",
			email:    "`id`@evil.com",
			fullName: "Test User",
			checkFunc: func(t *testing.T, cmd string) {
				t.Helper()
				if !containsSubstring(cmd, "'`id`@evil.com'") {
					t.Errorf("expected backtick email to be inside single quotes, got: %s", cmd)
				}
			},
		},
		{
			name:     "single quote in name",
			email:    "test@example.com",
			fullName: "O'Brien",
			checkFunc: func(t *testing.T, cmd string) {
				t.Helper()
				if !containsSubstring(cmd, "'O'\\''Brien'") {
					t.Errorf("expected single quote escaped via POSIX quoting, got: %s", cmd)
				}
			},
		},
		{
			name:     "newline in name",
			email:    "test@example.com",
			fullName: "Test\nUser",
			checkFunc: func(t *testing.T, cmd string) {
				t.Helper()
				if !containsSubstring(cmd, "'Test\nUser'") {
					t.Errorf("expected newline inside single quotes, got: %s", cmd)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			authInfo := auth.Info{
				Token:    "test-token",
				APIHost:  "api.app-prg1.zerops.io",
				Region:   "prg1",
				Email:    tt.email,
				FullName: tt.fullName,
			}
			id := GitIdentity{Name: tt.fullName, Email: tt.email}
			cmd := buildSSHCommand(authInfo, "svc-target", "/var/www", false, id)
			tt.checkFunc(t, cmd)
		})
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
	result, err := Deploy(context.Background(), mock, "proj-1", ssh, authInfo,
		"", "app", "", false)
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

func TestDeploy_SelfDeploy_IncludeGitForced(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "app"},
		})
	ssh := &mockSSHDeployer{output: []byte("ok")}
	authInfo := testAuthInfo()

	// Self-deploy (source == target): includeGit should be forced to true even if false is passed.
	result, err := Deploy(context.Background(), mock, "proj-1", ssh, authInfo,
		"app", "app", "", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Mode != "ssh" {
		t.Errorf("mode = %s, want ssh", result.Mode)
	}
	// Verify SSH command contains -g flag (includeGit forced).
	if len(ssh.calls) != 1 {
		t.Fatalf("ssh calls = %d, want 1", len(ssh.calls))
	}
	cmd := ssh.calls[0].command
	if !containsSubstring(cmd, " -g") {
		t.Errorf("SSH command should contain -g flag for self-deploy, got: %s", cmd)
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
	_, err := Deploy(context.Background(), mock, "proj-1", nil, authInfo,
		"", "app", "", false)
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

			_, err := Deploy(context.Background(), mock, "proj-1", ssh, authInfo,
				"app", "app", tt.workingDir, false)

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

	_, err := Deploy(context.Background(), mock, "proj-1", nil, authInfo,
		"builder", "app", "", false)
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
