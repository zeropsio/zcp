// Tests for: ops/deploy.go — Deploy local mode and mode detection.
package ops

import (
	"context"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

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
