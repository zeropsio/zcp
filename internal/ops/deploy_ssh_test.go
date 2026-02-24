// Tests for: ops/deploy.go — Deploy with SSH and local modes.
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

type zcliCall struct {
	args []string
}

type mockLocalDeployer struct {
	output []byte
	err    error
	errs   []error // per-call errors; takes precedence over err when set
	calls  []zcliCall
}

func (m *mockLocalDeployer) ExecZcli(_ context.Context, args ...string) ([]byte, error) {
	idx := len(m.calls)
	m.calls = append(m.calls, zcliCall{args: args})
	if idx < len(m.errs) {
		return m.output, m.errs[idx]
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
				tt.sourceService, tt.targetService, tt.setup, tt.workingDir, false, false)
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
		"nonexistent", "app", "", "", false, false)
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
		"builder", "nonexistent", "", "", false, false)
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
		"builder", "app", "", "", false, false)
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
	ssh := &mockSSHDeployer{err: fmt.Errorf("process exited: signal: killed")}
	local := &mockLocalDeployer{}
	authInfo := testAuthInfo()

	_, err := Deploy(context.Background(), mock, "proj-1", ssh, local, authInfo,
		"builder", "app", "", "", false, false)
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
	if !containsSubstring(pe.Suggestion, "RAM") {
		t.Errorf("suggestion should mention RAM scaling, got: %s", pe.Suggestion)
	}
}

func TestDeploy_SSHMode_CommandNotFound(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "builder"},
			{ID: "svc-2", Name: "app"},
		})
	ssh := &mockSSHDeployer{err: fmt.Errorf("bash: zcli: command not found")}
	local := &mockLocalDeployer{}
	authInfo := testAuthInfo()

	_, err := Deploy(context.Background(), mock, "proj-1", ssh, local, authInfo,
		"builder", "app", "", "", false, false)
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
	if !containsSubstring(pe.Suggestion, "zcli") {
		t.Errorf("suggestion should mention zcli, got: %s", pe.Suggestion)
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
	local := &mockLocalDeployer{}
	authInfo := testAuthInfo()

	_, err := Deploy(context.Background(), mock, "proj-1", ssh, local, authInfo,
		"builder", "app", "", "", false, false)
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
	local := &mockLocalDeployer{}
	authInfo := auth.Info{
		Token:   "test-token",
		APIHost: "api.app-fra1.zerops.io",
		Region:  "fra1",
	}

	result, err := Deploy(context.Background(), mock, "proj-1", ssh, local, authInfo,
		"builder", "app", "", "", false, false)
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
				err:    fmt.Errorf("ssh builder: process exited with status 255"),
			}
			local := &mockLocalDeployer{}
			authInfo := testAuthInfo()

			result, err := Deploy(context.Background(), mock, "proj-1", ssh, local, authInfo,
				"builder", "app", "", "", false, false)
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
				err:    fmt.Errorf("ssh builder: process exited with status 255"),
			}
			local := &mockLocalDeployer{}
			authInfo := testAuthInfo()

			_, err := Deploy(context.Background(), mock, "proj-1", ssh, local, authInfo,
				"builder", "app", "", "", false, false)
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

func TestClassifySSHError_ZeropsYmlNotFound(t *testing.T) {
	t.Parallel()

	err := fmt.Errorf("File zerops.yml not found in /var/www")
	pe := classifySSHError(err, "builder", "app")
	if pe.Code != platform.ErrSSHDeployFailed {
		t.Errorf("code = %s, want %s", pe.Code, platform.ErrSSHDeployFailed)
	}
	if !containsSubstring(pe.Suggestion, "deployFiles") {
		t.Errorf("suggestion should mention deployFiles, got: %s", pe.Suggestion)
	}
}

func TestClassifySSHError_ConnectionRefused(t *testing.T) {
	t.Parallel()

	err := fmt.Errorf("connection refused")
	pe := classifySSHError(err, "builder", "app")
	if pe.Code != platform.ErrSSHDeployFailed {
		t.Errorf("code = %s, want %s", pe.Code, platform.ErrSSHDeployFailed)
	}
	if !containsSubstring(pe.Suggestion, "RUNNING") {
		t.Errorf("suggestion should mention RUNNING, got: %s", pe.Suggestion)
	}
}

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

func TestDeploy_NilSSHDeployer(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-1", Name: "builder"},
			{ID: "svc-2", Name: "app"},
		})
	authInfo := testAuthInfo()

	_, err := Deploy(context.Background(), mock, "proj-1", nil, nil, authInfo,
		"builder", "app", "", "", false, false)
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
